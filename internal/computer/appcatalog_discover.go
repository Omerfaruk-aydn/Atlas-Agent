}

func sortProfiles(p []BrowserProfile) {
	for i := 1; i < len(p); i++ {
		for j := i; j > 0 && p[j].Directory < p[j-1].Directory; j-- {
			p[j], p[j-1] = p[j-1], p[j]
		}
	}
}
