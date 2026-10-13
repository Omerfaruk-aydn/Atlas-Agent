
func (b *limitedBuffer) String() string { return string(b.Bytes()) }

// Sanitize shortens CLI text for display and logs. It drops control
// characters and anything that looks like an auth key; Atlas never passes
// one, but the user's environment might.
func Sanitize(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	fields := strings.Fields(s)
	for i, f := range fields {
		if strings.HasPrefix(f, "tskey-") || strings.Contains(f, "--auth-key") || strings.Contains(f, "--authkey") {
			fields[i] = "[redacted]"
		}
	}
	s = strings.Join(fields, " ")
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}
