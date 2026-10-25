package task

import "sort"

func Insert(xs []int, v int) []int {
	out := append([]int{}, xs...)
	out = append(out, v)
	sort.Ints(out)
	return out
}
