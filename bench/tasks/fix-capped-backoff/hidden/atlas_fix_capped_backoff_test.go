package task

import (
	"reflect"
	"testing"
)

func TestAtlasFixCappedBackoff(t *testing.T) {
	for _, c := range []struct{ b, c, a, w int }{{2, 100, 3, 16}, {2, 100, 100, 100}, {0, 100, 1, 0}, {5, 3, 0, 3}} {
		if !reflect.DeepEqual(Backoff(c.b, c.c, c.a), c.w) {
			t.Fatal(c)
		}
	}
}
