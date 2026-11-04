package webui

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/dots"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/proto"
)

// Limits of watch triggers.
const (
	maxWatchFiles  = 2000
	maxPageBytes   = 2 << 20
	pageTimeout    = 20 * time.Second
	pageExcerpt    = 3000
	approvalIdle   = 3 * time.Second
	approvalBackof = 15 * time.Second
)

// checkWatches looks at every watch trigger that is due and fires those
// whose files or page changed since the last look.
func (r *runner) checkWatches(byID map[string]dots.Dot, now time.Time) {
	for _, t := range r.g.opts.Automation.Watched(now) {
		d, ok := byID[t.DotID]
		if !ok {
			continue
		}
		go r.checkWatch(d, t)
	}
}

func (r *runner) checkWatch(d dots.Dot, t dots.Trigger) {
	g := r.g
	var (
		fp, extra string
		err       error
	)
	switch t.Kind {
	case dots.KindFile:
		fp, extra, err = fileFingerprint(d.Workspace, t.Path)
	case dots.KindPage:
		ctx, cancel := context.WithTimeout(g.ctx, pageTimeout)
		fp, extra, err = pageFingerprint(ctx, t.URL)
		cancel()
	default:
		return
	}
	if err != nil {
		g.logger.Info("Dot watch check failed", "dot", d.ID, "trigger", t.ID, "error", err)
		return
	}
	if !g.opts.Automation.Observe(d.ID, t.ID, fp) {
		return
	}
	if _, err := r.Fire(d, t, dots.SourceWatch, extra); err != nil {
		g.logger.Warn("Dot watch trigger did not start", "dot", d.ID, "trigger", t.ID, "error", err)
	}
}

// rebaseline takes a file trigger's current state as its baseline after
// the run it started, so files the dot wrote in answer do not start another
// run of the same trigger, forever.
func (r *runner) rebaseline(d dots.Dot, triggerID string) {
	t, err := r.g.opts.Automation.Trigger(d.ID, triggerID)
	if err != nil || t.Kind != dots.KindFile {
		return
	}
	if fp, _, err := fileFingerprint(d.Workspace, t.Path); err == nil {
		r.g.opts.Automation.Rebaseline(d.ID, t.ID, fp)
	}
}

// fileFingerprint hashes the names, sizes and times of the files matching
// pattern inside root. A pattern naming a directory covers everything in
// it. It returns the fingerprint and a list of the files for the dot.
func fileFingerprint(root, pattern string) (string, string, error) {
	if err := dots.ValidWatchPath(pattern); err != nil {
		return "", "", err
	}
	full := filepath.Join(root, filepath.FromSlash(pattern))
	var paths []string
	if st, err := os.Stat(full); err == nil && st.IsDir() {
		_ = filepath.WalkDir(full, func(p string, e fs.DirEntry, err error) error {
			if err != nil || len(paths) >= maxWatchFiles {
				return filepath.SkipDir
			}
			if e.IsDir() && strings.HasPrefix(e.Name(), ".") && p != full {
				return filepath.SkipDir
			}
			if !e.IsDir() {
				paths = append(paths, p)
			}
			return nil
		})
	} else {
		matches, err := filepath.Glob(full)
		if err != nil {
			return "", "", err
		}
		paths = matches
	}
	slices.Sort(paths)
	h := sha256.New()
	var list strings.Builder
	for i, p := range paths {
		if i >= maxWatchFiles {
			break
		}
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			continue
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		fmt.Fprintf(h, "%s\x00%d\x00%d\n", rel, st.Size(), st.ModTime().UnixNano())
		if i < 50 {
			fmt.Fprintf(&list, "- %s (%d bayt, %s)\n", rel, st.Size(), st.ModTime().Format("2006-01-02 15:04"))
		}
	}
	extra := "İzlenen dosyalar değişti (" + pattern + "). Şu an eşleşenler:\n" + list.String()
	if list.Len() == 0 {
		extra = "İzlenen dosyalar (" + pattern + ") silindi ya da artık eşleşen dosya yok."
	}
	return hex.EncodeToString(h.Sum(nil)), extra, nil
}

var (
	reScript = regexp.MustCompile(`(?is)<(script|style|noscript|svg)\b.*?</(script|style|noscript|svg)>`)
	reTag    = regexp.MustCompile(`(?s)<[^>]*>`)
	reSpace  = regexp.MustCompile(`\s+`)
)

// pageText reduces an HTML page to its visible words, so a rotating ad
// token or a script nonce does not count as a change.
func pageText(html string) string {
	s := reScript.ReplaceAllString(html, " ")
	s = reTag.ReplaceAllString(s, " ")
	for _, r := range [][2]string{{"&nbsp;", " "}, {"&amp;", "&"}, {"&lt;", "<"}, {"&gt;", ">"}, {"&quot;", `"`}, {"&#39;", "'"}} {
		s = strings.ReplaceAll(s, r[0], r[1])
	}
	return strings.TrimSpace(reSpace.ReplaceAllString(s, " "))
}

func pageFingerprint(ctx context.Context, raw string) (string, string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", "", fmt.Errorf("not an http address")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "AtlasDots/1 (+page watch)")
	req.Header.Set("Accept", "text/html,text/plain;q=0.9,*/*;q=0.5")
	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer rsp.Body.Close()
	if rsp.StatusCode >= 400 {
		return "", "", fmt.Errorf("page returned %d", rsp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(rsp.Body, maxPageBytes))
	if err != nil {
		return "", "", err
	}
	text := pageText(string(body))
	sum := sha256.Sum256([]byte(text))
	extra := "İzlenen sayfa değişti: " + u.String() + "\nSayfanın şu anki metninden bir bölüm:\n" + truncateRunes(text, pageExcerpt)
	return hex.EncodeToString(sum[:]), extra, nil
}

// --- Approvals away from the screen ---

// ask is a permission request that was sent to the owner's phone.
type ask struct {
	dotID, wid, permID, ref string
}

// relayApprovals sends every new permission request of an open dot to the
// owner's phone, or as a plain notice without an approval channel, and
// clears messages for requests answered on the screen.
func (r *runner) relayApprovals(byID map[string]dots.Dot) {
	g := r.g
	live := map[string]bool{}
	for _, d := range byID {
		if d.SessionID == "" {
			continue
		}
		h := g.hubForPath(d.Workspace)
		if h == nil {
			continue
		}
		wid := url.PathEscape(h.workspaceID())
		ctx, cancel := context.WithTimeout(g.ctx, upstreamTimeout)
		var pending []proto.PermissionRequest
		err := g.getJSON(ctx, "/workspaces/"+wid+"/permissions/pending", nil, &pending)
		cancel()
		if err != nil {
			continue
		}
		for _, p := range pending {
			if p.SessionID != d.SessionID {
				continue
			}
			live[p.ID] = true
			r.mu.Lock()
			_, sent := r.asked[p.ID]
			if !sent {
				r.asked[p.ID] = &ask{dotID: d.ID, wid: wid, permID: p.ID}
			}
			r.mu.Unlock()
			if !sent {
				r.sendApproval(d, p)
			}
		}
	}
	r.mu.Lock()
	var gone []*ask
	for id, a := range r.asked {
		if !live[id] {
			gone = append(gone, a)
			delete(r.asked, id)
			delete(r.keys, keyOf(r.keys, id))
		}
	}
	r.mu.Unlock()
	for _, a := range gone {
		if a.ref != "" && g.opts.Approvals != nil {
			ctx, cancel := context.WithTimeout(g.ctx, upstreamTimeout)
			_ = g.opts.Approvals.Close(ctx, a.ref, "Ekrandan yanıtlandı")
			cancel()
		}
	}
}

func keyOf(keys map[string]string, permID string) string {
	for k, v := range keys {
		if v == permID {
			return k
		}
	}
	return ""
}

func (r *runner) sendApproval(d dots.Dot, p proto.PermissionRequest) {
	g := r.g
	title := firstNonEmpty(p.Description, p.ToolName)
	if g.opts.Approvals == nil {
		g.notify(Notice{Dot: d.Name, Title: d.Name + " onay bekliyor", Body: truncateRunes(title, 240)})
		return
	}
	var b [5]byte
	_, _ = rand.Read(b[:])
	key := hex.EncodeToString(b[:])
	detail := p.Path
	if detail == "" && p.Params != nil {
		if raw, err := json.Marshal(p.Params); err == nil && string(raw) != "null" {
			detail = truncateRunes(string(raw), 600)
		}
	}
	go func() {
		ctx, cancel := context.WithTimeout(g.ctx, upstreamTimeout)
		defer cancel()
		ref, err := g.opts.Approvals.Ask(ctx, Approval{Dot: d.Name, Title: p.ToolName + ": " + title, Detail: detail, Key: key})
		if err != nil {
			g.logger.Warn("Failed to send dot approval", "error", err)
			return
		}
		r.mu.Lock()
		if a, ok := r.asked[p.ID]; ok {
			a.ref = ref
			r.keys[key] = p.ID
		}
		r.mu.Unlock()
	}()
}

// answerApprovals reads the owner's button presses and applies them,
// until the gateway shuts down.
func (r *runner) answerApprovals() {
	g := r.g
	ch := g.opts.Approvals
	if ch == nil {
		return
	}
	for g.ctx.Err() == nil {
		r.mu.Lock()
		waiting := len(r.keys) > 0
		r.mu.Unlock()
		if !waiting {
			sleepCtx(g.ctx, approvalIdle)
			continue
		}
		answers, err := ch.Answers(g.ctx)
		if err != nil {
			if g.ctx.Err() == nil {
				g.logger.Info("Failed to read approval answers", "error", err)
				sleepCtx(g.ctx, approvalBackof)
			}
			continue
		}
		for _, ans := range answers {
			r.mu.Lock()
			permID := r.keys[ans.Key]
			a := r.asked[permID]
			if a != nil {
				delete(r.keys, ans.Key)
				delete(r.asked, permID)
			}
			r.mu.Unlock()
			if a == nil {
				continue
			}
			ctx, cancel := context.WithTimeout(g.ctx, upstreamTimeout)
			resolved, err := g.grantPermission(ctx, a.wid, a.permID, proto.PermissionAction(ans.Action))
			label := map[string]string{"allow": "✅ İzin verildi", "allow_session": "✅ Oturum boyunca izinli", "deny": "❌ Reddedildi"}[ans.Action]
			switch {
			case err != nil:
				label = "Uygulanamadı: " + truncateRunes(err.Error(), 60)
			case !resolved:
				label = "Bu istek artık beklemiyor"
			}
			_ = ch.Close(ctx, firstNonEmpty(a.ref, ans.Ref), label)
			cancel()
		}
	}
}

// grantPermission answers a pending permission request of workspace wid.
// It reports false when the request is no longer pending.
func (g *Gateway) grantPermission(ctx context.Context, wid, permID string, action proto.PermissionAction) (bool, error) {
	switch action {
	case proto.PermissionAllow, proto.PermissionAllowForSession, proto.PermissionDeny:
	default:
		return false, fmt.Errorf("action must be allow, allow_session or deny")
	}
	var pending []proto.PermissionRequest
	if err := g.getJSON(ctx, "/workspaces/"+wid+"/permissions/pending", nil, &pending); err != nil {
		return false, err
	}
	i := slices.IndexFunc(pending, func(p proto.PermissionRequest) bool { return p.ID == permID })
	if i < 0 {
		// Answered elsewhere, or withdrawn because its run ended.
		return false, nil
	}
	var out proto.PermissionGrantResponse
	if err := g.sendJSON(ctx, http.MethodPost, "/workspaces/"+wid+"/permissions/grant", nil, proto.PermissionGrant{Permission: pending[i], Action: action}, &out); err != nil {
		return false, err
	}
	return out.Resolved, nil
}

func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
