package task

func Live(xs []int, now, ttl int) []int {
	out := []int{}
	if ttl <= 0 {
		return out
	}
	for _, x := range xs {
		if x > now-ttl {
			out = append(out, x)
		}
	}
	return out
}
