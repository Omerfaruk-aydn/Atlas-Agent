package localapps

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/db"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/procown"
)

// capBuffer keeps the last limit bytes written.
type capBuffer struct {
	mu    sync.Mutex
	buf   []byte
	limit int
}

func (c *capBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buf = append(c.buf, p...)
	if len(c.buf) > c.limit {
		c.buf = c.buf[len(c.buf)-c.limit:]
	}
	return len(p), nil
}

func (c *capBuffer) tail(lines int) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	parts := strings.Split(strings.TrimRight(string(c.buf), "\r\n"), "\n")
	if len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	return strings.Join(parts, "\n")
}

// runCommand runs argv as an owned process tree with a deadline.
func runCommand(ctx context.Context, argv []string, dir string, env []string, timeout time.Duration) (string, error) {
	out := &capBuffer{limit: 32 << 10}
	proc, err := procown.Start(procown.Spec{Argv: argv, Dir: dir, Env: env, Stdout: out, Stderr: out})
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	code, err := proc.Wait(ctx)
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return out.tail(30), fmt.Errorf("did not finish within %s", timeout)
	case err != nil && ctx.Err() != nil:
		return out.tail(30), ctx.Err()
	case code != 0:
		return out.tail(30), fmt.Errorf("exited with code %d", code)
	}
	return out.tail(30), nil
}

// Verify builds, tests, starts and exercises the app against a copy of
// its data, restarts it to prove persistence, and records the result.
// The user's real data and a running copy of the app are never touched.
func (m *Manager) Verify(ctx context.Context, id string) (Verification, error) {
	release, err := m.acquire(id, "verify")
	if err != nil {
		return Verification{}, err
	}
	defer release()
	rec, err := m.record(id)
	if err != nil {
		return Verification{}, err
	}
	m.continueJournal(id, "verify")
	v, err := m.verifyLocked(ctx, rec)
	if err != nil {
		m.finishJournal(id, err)
		return v, err
	}
	if !v.Passed {
		m.finishJournal(id, errors.New(cmp.Or(v.Error, "verification failed")))
	} else {
		m.finishJournal(id, nil)
	}
	return v, nil
}

func (m *Manager) verifyLocked(ctx context.Context, rec Record) (Verification, error) {
	prevState := rec.State
	if _, err := m.mutate(ctx, rec.ID, func(r *Record) error {
		r.State = StateVerifying
		return nil
	}); err != nil {
		return Verification{}, err
	}
	v := m.runVerification(ctx, rec)
	if errors.Is(ctx.Err(), context.Canceled) {
		_, _ = m.mutate(context.Background(), rec.ID, func(r *Record) error {
			r.State = prevState
			return nil
		})
		return v, ctx.Err()
	}
	_, err := m.mutate(context.Background(), rec.ID, func(r *Record) error {
		r.LastVerification = &v
		r.ManifestHash = manifestHash(r.Root)
		if man, err := LoadManifest(r.Root); err == nil && man.Spec != nil {
			r.Spec = man.Spec
			r.Name, r.Description = man.Name, man.Description
		}
		switch {
		case v.Passed && acceptanceComplete(r, v.SourceHash):
			r.State = StateReady
		case v.Passed:
			r.State = StateVerified
		case prevState == StateUpdating || prevState == StateUpdateFailed || r.Update != nil:
			r.State = StateUpdateFailed
		default:
			r.State = StateFailed
		}
		if v.Passed {
			r.LastError = ""
		} else {
			r.LastError = v.Error
		}
		return nil
	})
	return v, err
}

func acceptanceComplete(r *Record, hash string) bool {
	if r.Spec == nil {
		return false
	}
	for i := range r.Spec.Acceptance {
		ok := false
		for _, a := range r.Acceptance {
			if a.Index == i+1 && a.Passed && a.SourceHash == hash {
				ok = true
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

func (m *Manager) runVerification(ctx context.Context, rec Record) Verification {
	v := Verification{At: time.Now()}
	fail := func(name StageName, check, detail string) Verification {
		v.Checks = append(v.Checks, Check{Name: check, Passed: false, Detail: detail})
		v.Error = check + ": " + detail
		m.stage(rec.ID, name, StageFailed, detail)
		return v
	}
	pass := func(name StageName, check, detail string) {
		v.Checks = append(v.Checks, Check{Name: check, Passed: true, Detail: detail})
		m.stage(rec.ID, name, StagePassed, detail)
	}
	man, err := LoadManifest(rec.Root)
	if err != nil {
		return fail(StageFiles, "manifest", err.Error())
	}
	if man.ID != rec.ID {
		return fail(StageFiles, "manifest", "the manifest id does not match this app")
	}
	v.SourceHash, err = sourceHash(rec.Root, man.DataDirs)
	if err != nil {
		return fail(StageFiles, "source fingerprint", err.Error())
	}
	rt, _ := RuntimeFor(man.Runtime)

	m.stage(rec.ID, StageDependencies, StageRunning, rt.Name)
	status := m.checkRuntime(ctx, man.Runtime)
	if !status.Available {
		return fail(StageDependencies, "runtime", cmp.Or(status.Problem, rt.Name+" is not available")+". "+cmp.Or(status.Remedy, rt.Remedy))
	}
	pass(StageDependencies, "runtime", strings.TrimSpace(rt.Name+" "+status.Version))

	tmp, err := os.MkdirTemp("", "atlas-app-verify-")
	if err != nil {
		return fail(StageStarting, "workspace", err.Error())
	}
	defer os.RemoveAll(tmp)
	baseEnv, err := m.appEnv(rec, man, 0, filepath.Join(tmp, "test-data"))
	if err != nil {
		return fail(StageStarting, "environment", err.Error())
	}
	cwd := rec.Root
	if man.Run.Cwd != "" && man.Run.Cwd != "." {
		cwd = filepath.Join(rec.Root, filepath.FromSlash(man.Run.Cwd))
	}

	if man.Build != nil {
		m.stage(rec.ID, StageBuilding, StageRunning, strings.Join(man.Build.Argv[1:], " "))
		out, err := runCommand(ctx, resolveArgv(man.Build.Argv, rt, status.Executable), cwd, baseEnv, time.Duration(cmp.Or(man.Build.TimeoutSeconds, 120))*time.Second)
		if err != nil {
			return fail(StageBuilding, "build", err.Error()+"\n"+out)
		}
		pass(StageBuilding, "build", "the code compiles")
	}
	if man.Test != nil {
		m.stage(rec.ID, StageTesting, StageRunning, strings.Join(man.Test.Argv[1:], " "))
		out, err := runCommand(ctx, resolveArgv(man.Test.Argv, rt, status.Executable), cwd, baseEnv, time.Duration(cmp.Or(man.Test.TimeoutSeconds, 300))*time.Second)
		if err != nil {
			return fail(StageTesting, "tests", err.Error()+"\n"+out)
		}
		pass(StageTesting, "tests", lastLine(out))
	}

	// Run against a consistent copy of the real data so migrations are
	// exercised on it without touching it.
	dataCopy := filepath.Join(tmp, "data")
	copied, err := copyDataForVerification(ctx, rec.Root, man, dataCopy)
	if err != nil {
		return fail(StageStarting, "data copy", err.Error())
	}
	port, err := pickPort(0)
	if err != nil {
		return fail(StageStarting, "port", err.Error())
	}
	m.stage(rec.ID, StageStarting, StageRunning, fmt.Sprintf("port %d", port))
	_, proc, err := m.launch(ctx, rec, man, rt, status.Executable, port, dataCopy, false)
	if err != nil {
		return fail(StageStarting, "start", startDetail(err))
	}
	startNote := "started and passed its health check"
	if copied {
		startNote += " with a copy of the current data"
	}
	pass(StageStarting, "start", startNote)

	m.stage(rec.ID, StageVerifying, StageRunning, "exercising the app over HTTP")
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	flow, err := exerciseApp(ctx, base, man)
	proc.Stop(5 * time.Second)
	if err != nil {
		return fail(StageVerifying, "user flow", err.Error())
	}
	pass(StageVerifying, "user flow", flow.summary)

	// Restart on the same data: the record must still be there.
	_, proc2, err := m.launch(ctx, rec, man, rt, status.Executable, port, dataCopy, false)
	if err != nil {
		return fail(StageVerifying, "restart", startDetail(err))
	}
	err = checkPersisted(ctx, base, flow)
	proc2.Stop(5 * time.Second)
	if err != nil {
		return fail(StageVerifying, "data after restart", err.Error())
	}
	pass(StageVerifying, "data after restart", "the record created before the restart was read back after it")
	v.Passed = true
	return v
}

func startDetail(err error) string {
	var op *OpError
	if errors.As(err, &op) && len(op.LogTail) > 0 {
		return op.Msg + "\n" + strings.Join(op.LogTail, "\n")
	}
	return err.Error()
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return "passed"
}

// copyDataForVerification snapshots databases and copies the other data
// files into dst. It reports whether any existing data was copied.
func copyDataForVerification(ctx context.Context, root string, man Manifest, dst string) (bool, error) {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return false, err
	}
	copied := false
	primary := filepath.Join(root, filepath.FromSlash(man.DataDirs[0]))
	for _, rel := range man.Backup.SQLite {
		src, err := insideRoot(root, rel)
		if err != nil {
			return false, err
		}
		if _, err := os.Stat(src); err != nil {
			continue
		}
		inner, err := filepath.Rel(primary, src)
		if err != nil || strings.HasPrefix(inner, "..") {
			continue
		}
		target := filepath.Join(dst, inner)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return false, err
		}
		if err := db.SnapshotSQLite(ctx, src, target); err != nil {
			return false, err
		}
		copied = true
	}
	err := filepath.WalkDir(primary, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || e.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		inner, _ := filepath.Rel(primary, p)
		target := filepath.Join(dst, inner)
		if _, err := os.Stat(target); err == nil || isSQLiteSidecar(p, root, man) {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		copied = true
		return os.WriteFile(target, data, 0o644)
	})
	return copied, err
}

func isSQLiteSidecar(p, root string, man Manifest) bool {
	for _, rel := range man.Backup.SQLite {
		db := filepath.Join(root, filepath.FromSlash(rel))
		for _, s := range []string{"", "-journal", "-wal", "-shm"} {
			if samePath(p, db+s) {
				return true
			}
		}
	}
	return false
}

type flowResult struct {
	entity  string
	id      any
	sent    map[string]any
	summary string
}

// sampleValue builds a valid value for a field type.
func sampleValue(f Field) any {
	switch f.Type {
	case "integer":
		return 3
	case "number":
		return 2.5
	case "date":
		return "2026-01-15"
	case "boolean":
		return true
	case "choice":
		return f.Choices[0]
	case "longtext":
		return "Atlas verification note"
	}
	return "Atlas check " + f.Name
}

func doJSON(ctx context.Context, method, url string, body any) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		rd = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return resp.StatusCode, data, err
}

// exerciseApp runs the basic user flow through the app's real HTTP API:
// load the page, create a record, find it in the list.
func exerciseApp(ctx context.Context, base string, man Manifest) (flowResult, error) {
	code, page, err := doJSON(ctx, http.MethodGet, base+cmp.Or(man.URLPath, "/"), nil)
	if err != nil {
		return flowResult{}, fmt.Errorf("the page did not load: %w", err)
	}
	if code != http.StatusOK || !bytes.Contains(bytes.ToLower(page), []byte("<html")) {
		return flowResult{}, fmt.Errorf("the page answered HTTP %d without an HTML document", code)
	}
	code, raw, err := doJSON(ctx, http.MethodGet, base+"/api/_schema", nil)
	if err != nil || code != http.StatusOK {
		return flowResult{}, fmt.Errorf("the app does not expose its data model at /api/_schema (HTTP %d, %v)", code, err)
	}
	var schema struct {
		Entities []Entity `json:"entities"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil || len(schema.Entities) == 0 {
		return flowResult{}, errors.New("the app's data model is empty or unreadable")
	}
	e := schema.Entities[0]
	sent := map[string]any{}
	for _, f := range e.Fields {
		sent[f.Name] = sampleValue(f)
	}
	code, raw, err = doJSON(ctx, http.MethodPost, base+"/api/"+e.Name, sent)
	if err != nil {
		return flowResult{}, fmt.Errorf("creating a %s failed: %w", e.Label, err)
	}
	if code != http.StatusCreated {
		return flowResult{}, fmt.Errorf("creating a %s answered HTTP %d: %s", e.Label, code, truncate(string(raw), 300))
	}
	var created map[string]any
	if err := json.Unmarshal(raw, &created); err != nil || created["id"] == nil {
		return flowResult{}, errors.New("the created record has no id")
	}
	code, raw, err = doJSON(ctx, http.MethodGet, base+"/api/"+e.Name, nil)
	if err != nil || code != http.StatusOK {
		return flowResult{}, fmt.Errorf("listing %s failed (HTTP %d, %v)", e.Label, code, err)
	}
	var list struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return flowResult{}, errors.New("the list answer is not readable JSON")
	}
	found := false
	for _, it := range list.Items {
		if fmt.Sprint(it["id"]) == fmt.Sprint(created["id"]) {
			found = true
		}
	}
	if !found {
		return flowResult{}, fmt.Errorf("the new %s is missing from the list", e.Label)
	}
	return flowResult{entity: e.Name, id: created["id"], sent: sent, summary: fmt.Sprintf("loaded the page, created a %s and found it in the list", e.Label)}, nil
}

func checkPersisted(ctx context.Context, base string, flow flowResult) error {
	code, raw, err := doJSON(ctx, http.MethodGet, fmt.Sprintf("%s/api/%s/%v", base, flow.entity, flow.id), nil)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("the record is gone after the restart (HTTP %d)", code)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		return errors.New("the record after the restart is not readable JSON")
	}
	for k, want := range flow.sent {
		if fmt.Sprint(normalize(got[k])) != fmt.Sprint(normalize(want)) {
			return fmt.Errorf("field %s changed across the restart: sent %v, read %v", k, want, got[k])
		}
	}
	return nil
}

func normalize(v any) any {
	switch x := v.(type) {
	case float64:
		if x == float64(int64(x)) {
			return int64(x)
		}
	case int:
		return int64(x)
	case bool:
		if x {
			return int64(1)
		}
		return int64(0)
	}
	return v
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// RecordAcceptance stores evidence for one acceptance criterion against the
// current files. The app becomes ready only when its verification passed
// and every criterion has passing evidence for the same files.
func (m *Manager) RecordAcceptance(ctx context.Context, id string, index int, passed bool, method, evidence string) (Record, error) {
	release, err := m.acquire(id, "acceptance")
	if err != nil {
		return Record{}, err
	}
	defer release()
	rec, err := m.record(id)
	if err != nil {
		return Record{}, err
	}
	if rec.Spec == nil || index < 1 || index > len(rec.Spec.Acceptance) {
		return Record{}, opErr(ErrInvalidState, "Use the 1-based index of a planned acceptance criterion.", "no acceptance criterion %d", index)
	}
	evidence = strings.TrimSpace(evidence)
	if evidence == "" {
		return Record{}, opErr(ErrInvalidState, "Describe what was observed.", "evidence is required")
	}
	man, err := LoadManifest(rec.Root)
	if err != nil {
		return Record{}, err
	}
	hash, err := sourceHash(rec.Root, man.DataDirs)
	if err != nil {
		return Record{}, err
	}
	return m.mutate(ctx, id, func(r *Record) error {
		a := Acceptance{Index: index, Criterion: r.Spec.Acceptance[index-1], Passed: passed, Method: method, Evidence: truncate(evidence, 1000), At: time.Now(), SourceHash: hash}
		replaced := false
		for i := range r.Acceptance {
			if r.Acceptance[i].Index == index {
				r.Acceptance[i] = a
				replaced = true
			}
		}
		if !replaced {
			r.Acceptance = append(r.Acceptance, a)
		}
		if r.LastVerification != nil && r.LastVerification.Passed && r.LastVerification.SourceHash == hash {
			if acceptanceComplete(r, hash) {
				r.State = StateReady
			} else if r.State == StateReady {
				r.State = StateVerified
			}
		}
		return nil
	})
}
