package task

import (
	"reflect"
	"testing"
)

func TestAtlasFixHeaderCase(t *testing.T) {
	h := map[string]string{"Content-Type": "text/plain"}
	if !reflect.DeepEqual(Header(h, "content-type"), "text/plain") {
		t.Fatal("case")
	}
	if Header(h, "other") != "" || len(h) != 1 {
		t.Fatal("mutation")
	}
}
