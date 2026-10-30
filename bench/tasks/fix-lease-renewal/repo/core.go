package task

func Active(now, expires int) bool { return expires >= now }
