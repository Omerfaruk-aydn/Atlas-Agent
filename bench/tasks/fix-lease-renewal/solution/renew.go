package task

func Renew(now, expires, delta int) int {
	if !Active(now, expires) || delta <= 0 {
		return expires
	}
	next := now + delta
	if next < expires {
		return expires
	}
	return next
}
