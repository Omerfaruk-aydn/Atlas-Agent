package task

import (
	"reflect"
	"testing"
)

func TestAtlasFixStableUnique(t *testing.T) {
	if !reflect.DeepEqual(Unique([]string{"b", "a", "b", "", "a"}), []string{"b", "a", ""}) {
		t.Fatal("order")
	}
	if len(Unique(nil)) != 0 {
		t.Fatal("empty")
	}
}
