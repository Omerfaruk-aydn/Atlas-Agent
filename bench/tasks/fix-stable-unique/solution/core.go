package task

func Unique(xs []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, s := range xs {
		if !seen[s] {
			out = append(out, s)
			seen[s] = true
		}
	}
	return out
}
