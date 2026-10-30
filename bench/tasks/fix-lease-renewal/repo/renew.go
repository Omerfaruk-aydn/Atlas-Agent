package task

func Renew(now, expires, delta int) int {
	if !Active(now, expires) || delta <= 0 {
		return expires
	}
	return now + delta
}
