	// overlapping credentials or accidentally change later matches.
	var out strings.Builder
	m := hmac.New(sha256.New, temporaryRedaction.key[:])
	var digest [32]byte
	last := 0
	for i := 0; i < len(value); {
		matched := 0
		for n, hashes := range temporaryRedaction.byLength {
			if n == 0 || i+n > len(value) || n <= matched {
				continue
			}
			m.Reset()
			_, _ = m.Write([]byte(value[i : i+n]))
			copy(digest[:], m.Sum(digest[:0]))
			if _, ok := hashes[digest]; ok {
				matched = n
			}
		}
		if matched == 0 {
			i++
			continue
		}
		out.WriteString(value[last:i])
		out.WriteString("[REDACTED]")
		i += matched
		last = i
	}
	if last == 0 {
		return value
	}
	out.WriteString(value[last:])
	return out.String()
}
