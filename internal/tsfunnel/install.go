			return true
		}
	}
	return false
}

// splitRDN splits a distinguished name at commas outside quotes.
func splitRDN(s string) []string {
	var out []string
	var cur strings.Builder
	quoted := false
	for _, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
			cur.WriteRune(r)
		case r == ',' && !quoted:
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	return append(out, cur.String())
}

func orString(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func orInt(n, def int64) int64 {
	if n <= 0 {
		return def
	}
	return n
}
