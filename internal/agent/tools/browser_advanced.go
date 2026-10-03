package tools

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

func isAdvancedBrowserAction(action string) bool {
	switch action {
	case "find", "assert", "semantic_click", "semantic_type", "tabs", "tab_new", "tab_select", "tab_close", "frames", "network", "capture_region", "upload", "download_start", "download_wait":
		return true
	}
	return false
}

func runAdvancedBrowser(ctx context.Context, sess browser.Session, action string, params BrowserParams, root string) (fantasy.ToolResponse, error) {
	driver, ok := sess.(browser.AdvancedSession)
	if !ok {
		return fantasy.NewTextErrorResponse("Advanced browser actions unavailable in this driver"), nil
	}
	p := params.Advanced
	p.Action = action
	if p.Selector == "" && params.Ref != "" {
		var err error
		p.Selector, err = resolveTargetSelector("click", params)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
	}
	if p.Selector == "" {
		p.Selector = params.Selector
	}
	if action == "upload" || action == "download_start" || action == "download_wait" {
		for i, path := range p.Paths {
			absolute, err := interactionPath(root, path)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			p.Paths[i] = absolute
			if action == "upload" {
				info, err := os.Stat(absolute)
				if err != nil || !info.Mode().IsRegular() {
					return fantasy.NewTextErrorResponse("Upload requires an existing regular workspace file"), nil
				}
			}
		}
	}
	data, image, err := driver.Advanced(ctx, p)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	if len(image) > 0 {
		return fantasy.NewImageResponse(image, "image/png"), nil
	}
	return fantasy.NewTextResponse(string(data)), nil
}

// interactionPath checks both lexical traversal and symlink ancestors.
func interactionPath(root, path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("file path is required")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("interaction files must remain within the workspace")
	}
	ancestor := path
	for {
		_, err := os.Lstat(ancestor)
		if err == nil {
			resolved, err := filepath.EvalSymlinks(ancestor)
			if err != nil {
				return "", err
			}
			rel, err = filepath.Rel(root, resolved)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return "", fmt.Errorf("interaction path escapes through a symlink")
			}
			break
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		next := filepath.Dir(ancestor)
		if next == ancestor {
			return "", fmt.Errorf("cannot resolve interaction path")
		}
		ancestor = next
	}
	return path, nil
}

var authEnvName = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,127}$`)

func runBrowserAuth(ctx context.Context, sess browser.Session, p BrowserParams) (fantasy.ToolResponse, error) {
	if !authEnvName.MatchString(p.SecretEnv) || !strings.HasPrefix(p.SecretEnv, "ATLAS_TOTP_") {
		return fantasy.NewTextErrorResponse("secret_env must name an authorized ATLAS_TOTP_* environment variable"), nil
	}
	seed := os.Getenv(p.SecretEnv)
	if seed == "" {
		return fantasy.NewTextErrorResponse("Authorized TOTP seed is not configured; hand off to the user"), nil
	}
	code, err := totp(seed, time.Now())
	if err != nil {
		return fantasy.NewTextErrorResponse("Invalid configured TOTP seed"), nil
	}
	selector, err := resolveTargetSelector("type", p)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	if err := ctx.Err(); err != nil {
		return fantasy.ToolResponse{}, err
	}
	if err := sess.Type(selector, code); err != nil {
		return fantasy.NewTextErrorResponse("Could not enter the authentication code"), nil
	}
	return fantasy.NewTextResponse("Authorized TOTP entered. Verify the login result; the code was not returned or recorded."), nil
}

// totp implements the standard six-digit SHA-1, 30-second RFC 6238 profile.
func totp(seed string, now time.Time) (string, error) {
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.TrimRight(strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(seed), " ", "")), "="))
	if err != nil || len(secret) < 10 {
		return "", fmt.Errorf("invalid seed")
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(now.Unix()/30))
	mac := hmac.New(sha1.New, secret)
	_, _ = mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 15
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", value%1000000), nil
}
