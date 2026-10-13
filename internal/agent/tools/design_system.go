	b.WriteString("\n## Color tokens\n\n")
	names := make([]string, 0, len(m.System.Colors))
	for n := range m.System.Colors {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(&b, "- `%s`: %s\n", n, m.System.Colors[n])
	}
	if len(m.OpenQuestions) > 0 {
		b.WriteString("\n## Open questions\n\n")
		for _, q := range m.OpenQuestions {
			fmt.Fprintf(&b, "- %s\n", q)
		}
	}
	return b.String()
}

func slug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "screen"
	}
	if len(out) > 40 {
		out = out[:40]
	}
	return out
}
