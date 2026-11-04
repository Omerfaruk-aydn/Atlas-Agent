// Package mailbox reads a saved mailbox over IMAP. The password stays in
// the caller; results never include it.
package mailbox

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	host    = "imap.gmail.com:993"
	maxMail = 8
)

// Message is one recent inbox item, trimmed for the agent.
type Message struct {
	From    string   `json:"from"`
	Subject string   `json:"subject"`
	Date    string   `json:"date,omitempty"`
	Excerpt string   `json:"excerpt,omitempty"`
	Codes   []string `json:"codes,omitempty"`
}

var (
	existsPattern = regexp.MustCompile(`(?m)^\* (\d+) EXISTS`)
	headerPattern = regexp.MustCompile(`(?mi)^(from|subject|date):\s*(.+)$`)
	codePattern   = regexp.MustCompile(`\b\d{4,8}\b`)
	hintPattern   = regexp.MustCompile(`(?i)(code|kod|otp|verification|doğrulama|onay)`)
)

// Check logs in and logs out. A rejected password returns an error and
// stores nothing.
func Check(ctx context.Context, address, secret string) error {
	conn, err := dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	c := &client{r: bufio.NewReader(conn), w: conn}
	if _, err = c.round(""); err != nil {
		return err
	}
	if err = c.login(address, normalizeSecret(secret)); err != nil {
		return err
	}
	_ = c.cmd("LOGOUT")
	return nil
}

// Recent returns the newest inbox messages. limit is capped.
func Recent(ctx context.Context, address, secret string, limit int) ([]Message, error) {
	if limit <= 0 || limit > maxMail {
		limit = maxMail
	}
	conn, err := dial(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return readRecent(conn, address, secret, limit)
}

func dial(ctx context.Context) (net.Conn, error) {
	d := tls.Dialer{Config: &tls.Config{ServerName: "imap.gmail.com", MinVersion: tls.VersionTLS12}}
	conn, err := d.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, errors.New("postaya ulaşılamadı")
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(20 * time.Second)
	}
	_ = conn.SetDeadline(deadline)
	return conn, nil
}

func readRecent(conn net.Conn, address, secret string, limit int) ([]Message, error) {
	c := &client{r: bufio.NewReader(conn), w: conn}
	if _, err := c.round(""); err != nil {
		return nil, err
	}
	if err := c.login(address, normalizeSecret(secret)); err != nil {
		return nil, err
	}
	selected, err := c.roundTrip("SELECT INBOX")
	if err != nil {
		return nil, err
	}
	exists := 0
	if m := existsPattern.FindStringSubmatch(selected); m != nil {
		exists, _ = strconv.Atoi(m[1])
	}
	if exists == 0 {
		_ = c.cmd("LOGOUT")
		return nil, nil
	}
	start := exists - limit + 1
	if start < 1 {
		start = 1
	}
	body, err := c.roundTrip(fmt.Sprintf("FETCH %d:%d (BODY.PEEK[HEADER.FIELDS (FROM SUBJECT DATE)] BODY.PEEK[TEXT]<0.2000>)", start, exists))
	if err != nil {
		return nil, err
	}
	_ = c.cmd("LOGOUT")
	return parseFetch(body), nil
}

func parseFetch(raw string) []Message {
	parts := regexp.MustCompile(`(?m)^\* \d+ FETCH `).Split(raw, -1)
	out := make([]Message, 0, len(parts))
	for _, part := range parts {
		msg := messageFrom(part)
		if msg.Subject == "" && msg.From == "" && msg.Excerpt == "" {
			continue
		}
		out = append(out, msg)
	}
	return out
}

func messageFrom(part string) Message {
	var msg Message
	for _, line := range strings.Split(part, "\n") {
		m := headerPattern.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		value := decodeHeader(strings.TrimSpace(m[2]))
		switch strings.ToLower(m[1]) {
		case "from":
			msg.From = value
		case "subject":
			msg.Subject = value
		case "date":
			msg.Date = value
		}
	}
	text := stripTags(part)
	text = strings.TrimSpace(text)
	if len(text) > 500 {
		text = text[:500]
	}
	msg.Excerpt = text
	msg.Codes = codes(msg.Subject + "\n" + text)
	return msg
}

func codes(text string) []string {
	found := codePattern.FindAllStringIndex(text, 12)
	var hinted, plain []string
	seen := map[string]bool{}
	for _, span := range found {
		value := text[span[0]:span[1]]
		if len(value) == 4 && (strings.HasPrefix(value, "19") || strings.HasPrefix(value, "20")) {
			continue
		}
		if seen[value] {
			continue
		}
		seen[value] = true
		before := text[max(0, span[0]-24):span[0]]
		if hintPattern.MatchString(before) {
			hinted = append(hinted, value)
		} else if len(value) >= 6 {
			plain = append(plain, value)
		}
	}
	if len(hinted) > 0 {
		return hinted
	}
	if len(plain) > 3 {
		plain = plain[:3]
	}
	return plain
}

func stripTags(s string) string {
	var b strings.Builder
	skip := false
	for _, r := range s {
		switch {
		case r == '<':
			skip = true
		case r == '>':
			skip = false
		case !skip:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func decodeHeader(s string) string {
	if !strings.Contains(s, "=?") {
		return trim(s)
	}
	return trim(s)
}

func trim(s string) string {
	s = strings.Trim(s, `"`)
	if !utf8.ValidString(s) {
		return strings.ToValidUTF8(s, "")
	}
	return s
}

// normalizeSecret removes the spaces Google inserts in an app password.
// A normal password is left as typed.
func normalizeSecret(s string) string {
	if strings.Count(s, " ") < 1 {
		return s
	}
	compact := strings.ReplaceAll(s, " ", "")
	if len(compact) == 16 && compact != "" && strings.Trim(compact, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ") == "" {
		return compact
	}
	return s
}

func (c *client) login(address, secret string) error {
	err := c.authPlain(address, secret)
	if err == nil || !unsupported(err) {
		return err
	}
	return c.cmd("LOGIN " + quote(address) + " " + quote(secret))
}

func unsupported(err error) bool {
	var status *statusError
	if !errors.As(err, &status) {
		return false
	}
	line := strings.ToLower(status.line)
	return strings.Contains(line, " bad") || strings.Contains(line, "unknown") || strings.Contains(line, "unrecognized") || strings.Contains(line, "not available")
}

func (c *client) authPlain(user, pass string) error {
	c.n++
	tag := fmt.Sprintf("A%d", c.n)
	if _, err := io.WriteString(c.w, tag+" AUTHENTICATE PLAIN\r\n"); err != nil {
		return errors.New("postaya ulaşılamadı")
	}
	for {
		line, _, err := c.piece()
		if err != nil {
			return errors.New("postaya ulaşılamadı")
		}
		if strings.HasPrefix(strings.TrimSpace(line), "+") {
			token := base64.StdEncoding.EncodeToString([]byte("\x00" + user + "\x00" + pass))
			if _, err = io.WriteString(c.w, token+"\r\n"); err != nil {
				return errors.New("postaya ulaşılamadı")
			}
			_, err = c.round(tag)
			return err
		}
		if strings.HasPrefix(line, tag+" ") {
			if strings.Contains(line, " OK") {
				return nil
			}
			return &statusError{line: line}
		}
	}
}

type statusError struct{ line string }

func (e *statusError) Error() string { return explain(e.line) }

func explain(line string) string {
	low := strings.ToLower(line)
	switch {
	case strings.Contains(low, "application-specific") || strings.Contains(low, "app password"):
		return "Google hesap şifresini posta kapısında kabul etmiyor. Web sitesine giren şifre burada geçmez; 16 haneli uygulama şifresi gerekir."
	case strings.Contains(low, "web login") || strings.Contains(low, "web browser") || strings.Contains(low, "signin"):
		return "Şifre doğru olabilir. Google bu kapıyı hesap şifresine kapatmış; posta için uygulama şifresi gerekir."
	case strings.Contains(low, "imap") && (strings.Contains(low, "disabled") || strings.Contains(low, "not enabled")):
		return "Gmail'de IMAP kapalı. Gmail ayarlarında Yönlendirme ve POP/IMAP bölümünden IMAP'i aç."
	case strings.Contains(low, "invalid credentials") || strings.Contains(low, "authenticationfailed"):
		return "Google bu şifreyi posta girişi olarak reddetti. Siteye girerken kullandığın şifre bu kapıda geçmez; uygulama şifresi gerekir."
	default:
		clean := urlPattern.ReplaceAllString(line, "")
		clean = strings.TrimSpace(clean)
		if clean == "" {
			return "Google postayı açmadı."
		}
		return "Google postayı açmadı: " + clean
	}
}

var urlPattern = regexp.MustCompile(`https://\S+`)

func quote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

type client struct {
	r *bufio.Reader
	w io.Writer
	n int
}

func (c *client) cmd(command string) error {
	_, err := c.roundTrip(command)
	return err
}

func (c *client) roundTrip(command string) (string, error) {
	c.n++
	tag := fmt.Sprintf("A%d", c.n)
	if _, err := io.WriteString(c.w, tag+" "+command+"\r\n"); err != nil {
		return "", errors.New("postaya ulaşılamadı")
	}
	return c.round(tag)
}

func (c *client) round(tag string) (string, error) {
	var b strings.Builder
	for {
		line, literal, err := c.piece()
		if err != nil {
			return "", errors.New("postaya ulaşılamadı")
		}
		b.WriteString(line)
		b.WriteString("\n")
		b.WriteString(literal)
		if tag != "" && strings.HasPrefix(line, tag+" ") {
			if strings.Contains(line, " OK") {
				return b.String(), nil
			}
			return "", &statusError{line: line}
		}
		if tag == "" && strings.HasPrefix(strings.TrimSpace(line), "*") {
			return b.String(), nil
		}
	}
}

func (c *client) piece() (string, string, error) {
	line, err := c.r.ReadString('\n')
	if err != nil {
		return "", "", err
	}
	line = strings.TrimRight(line, "\r\n")
	open := strings.LastIndex(line, "{")
	if open < 0 || !strings.HasSuffix(line, "}") {
		return line, "", nil
	}
	n, err := strconv.Atoi(line[open+1 : len(line)-1])
	if err != nil || n < 0 || n > 1<<20 {
		return line, "", nil
	}
	buf := make([]byte, n)
	if _, err = io.ReadFull(c.r, buf); err != nil {
		return "", "", err
	}
	_, _ = c.r.ReadString('\n')
	return line, string(buf), nil
}
