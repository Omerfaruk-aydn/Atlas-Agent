package export

import (
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
)

// HTML renders a self-contained, network-free and script-free local document.
func HTML(sess session.Session, msgs []message.Message) string {
	var b strings.Builder
	b.WriteString("<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width\"><title>")
	b.WriteString(html.EscapeString(nonEmpty(sess.Title, "Untitled session")))
	b.WriteString("</title><style>body{max-width:960px;margin:2rem auto;padding:0 1rem;font:16px/1.55 system-ui;background:#101217;color:#e7e9ee}article{border-top:1px solid #343a46;padding:1rem 0}pre{white-space:pre-wrap;overflow-wrap:anywhere;background:#181c24;padding:1rem}details{margin:.5rem 0}summary{cursor:pointer}.meta{color:#aab2c0}</style></head><body>")
	fmt.Fprintf(&b, "<h1>%s</h1><p class=\"meta\">Exported %s · %d messages · %.6f cost</p>", html.EscapeString(nonEmpty(sess.Title, "Untitled session")), time.Now().UTC().Format(time.RFC3339), len(msgs), sess.Cost)
	for _, msg := range msgs {
		fmt.Fprintf(&b, "<article><h2>%s</h2>", html.EscapeString(roleHeading(msg.Role)))
		if text := strings.TrimSpace(msg.Content().Text); text != "" {
			fmt.Fprintf(&b, "<pre>%s</pre>", html.EscapeString(text))
		}
		for _, call := range msg.ToolCalls() {
			fmt.Fprintf(&b, "<details><summary>Tool call: %s</summary><pre>%s</pre></details>", html.EscapeString(call.Name), html.EscapeString(truncate(call.Input)))
		}
		for _, result := range msg.ToolResults() {
			fmt.Fprintf(&b, "<details><summary>Tool result: %s</summary><pre>%s</pre></details>", html.EscapeString(result.Name), html.EscapeString(truncate(result.Content)))
		}
		b.WriteString("</article>")
	}
	b.WriteString("</body></html>")
	return RedactText(b.String())
}
