package task

func Overlap(a, b, c, d int) bool { return a < b && c < d && a < d && c < b }
