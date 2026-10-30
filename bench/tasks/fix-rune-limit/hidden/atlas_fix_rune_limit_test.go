package task

import (
	"reflect"
	"testing"
)

func TestAtlasFixRuneLimit(t *testing.T) {
	for _, c := range []struct {
		s    string
		n    int
		want string
	}{{"hé🙂x", 3, "hé🙂"}, {"a", 2, "a"}, {"a", 0, ""}} {
		if got := Truncate(c.s, c.n); !reflect.DeepEqual(got, c.want) {
			t.Fatalf("%q != %q", got, c.want)
		}
	}
}
