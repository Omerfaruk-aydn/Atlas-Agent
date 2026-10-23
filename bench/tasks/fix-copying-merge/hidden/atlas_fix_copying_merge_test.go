package task

import (
	"reflect"
	"testing"
)

func TestAtlasFixCopyingMerge(t *testing.T) {
	a := map[string]int{"x": 1}
	r := Merge(a, map[string]int{"x": 2, "y": 3})
	if !reflect.DeepEqual(r, map[string]int{"x": 2, "y": 3}) {
		t.Fatal(r)
	}
	r["x"] = 9
	if a["x"] != 1 {
		t.Fatal("alias")
	}
	if len(Merge(nil, nil)) != 0 {
		t.Fatal("nil")
	}
}
