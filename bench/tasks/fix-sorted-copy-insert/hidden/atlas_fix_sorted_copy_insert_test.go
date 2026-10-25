package task

import (
	"reflect"
	"testing"
)

func TestAtlasFixSortedCopyInsert(t *testing.T) {
	xs := []int{1, 4, 9}
	out := Insert(xs, 3)
	if !reflect.DeepEqual(out, []int{1, 3, 4, 9}) {
		t.Fatal(out)
	}
	if !reflect.DeepEqual(xs, []int{1, 4, 9}) {
		t.Fatal("mutated")
	}
	if !reflect.DeepEqual(Insert([]int{2}, 2), []int{2, 2}) {
		t.Fatal("duplicates")
	}
}
