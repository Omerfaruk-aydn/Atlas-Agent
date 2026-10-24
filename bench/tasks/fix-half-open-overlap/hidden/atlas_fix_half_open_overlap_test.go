package task

import (
	"reflect"
	"testing"
)

func TestAtlasFixHalfOpenOverlap(t *testing.T) {
	for _, c := range []struct {
		a, b, c, d int
		want       bool
	}{{1, 2, 2, 3, false}, {1, 4, 2, 3, true}, {1, 1, 0, 3, false}, {4, 2, 1, 5, false}} {
		if !reflect.DeepEqual(Overlap(c.a, c.b, c.c, c.d), c.want) {
			t.Fatal(c)
		}
	}
}
