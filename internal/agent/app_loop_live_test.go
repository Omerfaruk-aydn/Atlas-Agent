package agent

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/devloop"
	"github.com/stretchr/testify/require"
)

const sampleServer = `const http = require("http");
const fs = require("fs");
const path = require("path");
const port = Number(process.argv[2]);
http.createServer((req, res) => {
  console.log(new Date().toISOString(), req.method, req.url);
  if (req.url === "/" || req.url === "/signup") {
    res.writeHead(200, {"Content-Type": "text/html"});
    res.end(fs.readFileSync(path.join(__dirname, "signup.html")));
    return;
  }
  res.writeHead(404); res.end();
}).listen(port, "127.0.0.1", () => console.log("listening on " + port));
`

// The bug: the success handler writes into an element that does not exist,
// so the user never sees the confirmation.
const signupPage = `<!doctype html><html><body>
<h1>Sign up</h1>
<form id="f"><input id="email" type="email"><button type="submit">Create account</button></form>
<div class="toast-success" hidden></div>
<script>
document.getElementById("f").addEventListener("submit", (e) => {
  e.preventDefault();
  const t = document.querySelector(".toast-done");
  if (t) { t.hidden = false; t.textContent = "Account created for " + document.getElementById("email").value; }
});
</script></body></html>
`

// TestLiveAppLoopFixesARealUIBug is the acceptance run for the live app
// loop: the app is a real Node process started by app_loop, the checks run
// in a real headless Chrome through the scenario runner, and the fix is a
// real file edit. Opt in with ATLAS_LIVE_APP_LOOP=1 (needs node and Chrome).
func TestLiveAppLoopFixesARealUIBug(t *testing.T) {
	if os.Getenv("ATLAS_LIVE_APP_LOOP") != "1" {
		t.Skip("set ATLAS_LIVE_APP_LOOP=1 to run against node and Chrome")
	}
	node, err := exec.LookPath("node")
	require.NoError(t, err)
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/signup"
	t.Setenv("ATLAS_DEVICES_DIR", t.TempDir())

	scenario := map[string]any{
		"id": "signup", "version": 1, "target": "web", "url": url, "width": 1024, "height": 768,
		"steps": []map[string]any{
			{"action": "type", "selector": "#email", "value": "ada@example.test"},
			{"action": "click", "selector": "button[type=submit]"},
		},
		"assertions": []map[string]any{
			{"kind": "visible", "selector": ".toast-success"},
			{"kind": "text", "selector": ".toast-success", "expected": "Account created"},
		},
	}
	f := wireOpenAI
	loop := "signup-fix"
	h := newPayloadCoordinator(t, f, "", `, "tools": {"devices": {"enabled": true}, "browser": {"enabled": true, "headless": true}}`,
		toolCallReply(f, "c1", "tool_search", map[string]any{"names": []string{"app_loop", "scenario", "browser"}}),
		toolCallReply(f, "c2", "app_loop", map[string]any{"action": "plan", "loop_id": loop, "max_iterations": 3, "criteria": []map[string]any{
			{"id": "toast", "text": "Submitting the form shows the success confirmation", "verify": "scenario", "assertions": []int{1, 2}},
			{"id": "email", "text": "A welcome email is delivered", "verify": "unverifiable", "reason": "no SMTP service in this environment"},
		}}),
		toolCallReply(f, "c3", "app_loop", map[string]any{"action": "start", "loop_id": loop, "argv": []string{node, "server.js", strconv.Itoa(port)}, "port": port, "url": url}),
		toolCallReply(f, "c4", "app_loop", map[string]any{"action": "ready", "loop_id": loop, "probe_kind": "http", "probe_url": url, "probe_contains": "Sign up", "timeout_ms": 20000}),
		toolCallReply(f, "c5", "app_loop", map[string]any{"action": "check", "loop_id": loop, "scenario": scenario}),
		toolCallReply(f, "c6", "view", map[string]any{"file_path": "signup.html"}),
		toolCallReply(f, "c7", "edit", map[string]any{"file_path": "signup.html", "old_string": `".toast-done"`, "new_string": `".toast-success"`}),
		toolCallReply(f, "c8", "app_loop", map[string]any{"action": "check", "loop_id": loop, "scenario": scenario, "change_note": "fixed the toast selector typo"}),
		toolCallReply(f, "c9", "app_loop", map[string]any{"action": "report", "loop_id": loop}),
		toolCallReply(f, "c10", "app_loop", map[string]any{"action": "stop", "loop_id": loop}),
		textReply(f, "done"),
	)
	require.NoError(t, os.WriteFile(filepath.Join(h.env.workingDir, "server.js"), []byte(sampleServer), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(h.env.workingDir, "signup.html"), []byte(signupPage), 0o644))
	// Scenario evidence is bound to a source fingerprint, which needs a
	// repository, as in a real project.
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.email=t@example.test", "-c", "user.name=t", "commit", "-qm", "sample"}} {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = h.env.workingDir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	h.coord.(*coordinator).permissions.SetSkipRequests(true)
	sessionID := newSession(t, h)
	_, err = h.coord.Run(t.Context(), sessionID, "Kayıt ekranını düzelt, uygulamayı çalıştır, gerçek kullanıcı akışını dene ve sorunları düzelt.")
	require.NoError(t, err)

	result := func(step int) map[string]any {
		body := stepBody(t, h, step)
		// The tool result is the last message of the request.
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			} `json:"messages"`
		}
		require.NoError(t, json.Unmarshal([]byte(body), &req))
		last := req.Messages[len(req.Messages)-1]
		text, _ := last.Content.(string)
		var out map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &out), "step %d result: %s", step, text)
		return out
	}
	start := result(3)
	require.Equal(t, "started", start["result"].(map[string]any)["outcome"])
	ready := result(4)
	require.Equal(t, true, ready["ok"], "%v", ready)

	first := result(5)
	t.Logf("first check: %v", first["iteration"])
	require.Equal(t, false, first["ok"], "the real UI must show the bug")
	crit := first["iteration"].(map[string]any)["criteria"].([]any)
	require.Equal(t, "failed", crit[0].(map[string]any)["result"])
	require.Equal(t, "unverified", crit[1].(map[string]any)["result"])

	second := result(8)
	t.Logf("second check: %v", second["iteration"])
	require.Equal(t, true, second["ok"], "the fix must pass in the real UI: %v", second)

	report := result(9)
	t.Logf("report: %v", report["criteria"])
	require.Equal(t, "completed", report["state"])
	rows := report["criteria"].([]any)
	require.Equal(t, "passed", rows[0].(map[string]any)["result"])
	require.Equal(t, "unverified", rows[1].(map[string]any)["result"])
	require.EqualValues(t, 2, report["iterations"])

	stop := result(10)
	require.Equal(t, "stopped", stop["outcome"])
	require.Eventually(t, func() bool { return !devloop.Listening(port) }, 10*time.Second, 100*time.Millisecond, "the owned app must be stopped")
	require.True(t, strings.Contains(stepBody(t, h, 10), "stopped"))
	edited, err := os.ReadFile(filepath.Join(h.env.workingDir, "signup.html"))
	require.NoError(t, err)
	require.Contains(t, string(edited), `".toast-success"`)
}
