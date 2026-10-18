package movavg

import "testing"

func TestSimpleFirstWindow(t *testing.T) {
	got := Simple([]float64{1, 2, 3, 4, 5, 6}, 3)
	if len(got) == 0 || got[0] != 2 {
		t.Fatalf("Simple first window = %v, want 2", got)
	}
}
