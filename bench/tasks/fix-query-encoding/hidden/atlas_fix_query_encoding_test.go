package task

import (
	"reflect"
	"testing"
)

func TestAtlasFixQueryEncoding(t *testing.T) {
	for _, c := range [][2]string{{"a b&c", "a+b%26c"}, {"x+y", "x%2By"}, {"", ""}} {
		if !reflect.DeepEqual(Query(c[0]), c[1]) {
			t.Fatal(c)
		}
	}
}
