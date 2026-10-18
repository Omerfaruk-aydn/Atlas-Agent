package pool

import (
	"sync/atomic"
	"testing"
)

func TestRunsAllTasks(t *testing.T) {
	p := New(2)
	var n atomic.Int64
	for range 5 {
		if err := p.Submit(func() { n.Add(1) }); err != nil {
			t.Fatal(err)
		}
	}
	p.Close()
	if n.Load() != 5 {
		t.Fatalf("ran %d tasks, want 5", n.Load())
	}
}
