package bucket

import (
	"testing"
	"time"
)

func TestStartsFull(t *testing.T) {
	now := time.Unix(0, 0)
	b := New(1, 3, func() time.Time { return now })
	for i := range 3 {
		if !b.Allow(1) {
			t.Fatalf("request %d refused", i)
		}
	}
	if b.Allow(1) {
		t.Fatal("fourth request should be refused")
	}
}
