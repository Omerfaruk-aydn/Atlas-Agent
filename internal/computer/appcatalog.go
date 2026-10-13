}

// closestProfiles keeps the partial matches sharing the most words with q:
// "ömer faruk aydın" prefers "Ömer Faruk" over "Ömer". Equal overlap stays
// ambiguous.
func closestProfiles(partial []BrowserProfile, q string) []BrowserProfile {
	best, out := 0, []BrowserProfile(nil)
	for _, p := range partial {
		n := 0
		for _, t := range strings.Fields(FoldName(p.Name)) {
			if slices.Contains(strings.Fields(q), t) {
				n++
			}
		}
		switch {
		case n > best:
			best, out = n, []BrowserProfile{p}
		case n == best:
			out = append(out, p)
		}
	}
	return out
}
