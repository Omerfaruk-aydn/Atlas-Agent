package task

func Merge(a, b map[string]int) map[string]int {
	for k, v := range b {
		a[k] = v
	}
	return a
}
