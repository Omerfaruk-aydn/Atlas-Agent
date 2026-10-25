package task

import (
	"reflect"
	"testing"
)

func TestAtlasFixTerminalLineCount(t *testing.T) {
	for _, c := range []struct {
		s string
		w int
	}{{"", 0}, {"a", 1}, {"a\n", 1}, {"a\r\nb", 2}, {"\n\n", 2}} {
		if !reflect.DeepEqual(Lines(c.s), c.w) {
			t.Fatal(c)
		}
	}
}
