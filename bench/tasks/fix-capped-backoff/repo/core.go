package task

func Backoff(base, cap, attempt int) int {
	if base <= 0 || cap <= 0 || attempt < 0 {
		return 0
	}
	for i := 0; i < attempt; i++ {
		base *= 2
	}
	if base > cap {
		return cap
	}
	return base
}
