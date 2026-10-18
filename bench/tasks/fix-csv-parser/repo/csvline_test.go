package csvline

import (
	"reflect"
	"testing"
)

func TestParsePlain(t *testing.T) {
	got, err := Parse("a,b,c")
	if err != nil || !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("Parse = %v, %v", got, err)
	}
}
