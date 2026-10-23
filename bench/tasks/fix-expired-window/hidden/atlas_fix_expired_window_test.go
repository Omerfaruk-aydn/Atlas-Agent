package task

import (
	"reflect"
	"testing"
)

func TestAtlasFixExpiredWindow(t *testing.T) {
	if !reflect.DeepEqual(Live([]int{9, 5, 8}, 10, 5), []int{9, 8}) {
		t.Fatal("boundary")
	}
	if len(Live([]int{99}, 10, 0)) != 0 {
		t.Fatal("ttl")
	}
}
