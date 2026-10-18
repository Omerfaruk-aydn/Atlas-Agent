package movavg

import (
	"math"
	"testing"
)

func hiddenEqual(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Abs(a[i]-b[i]) > 1e-9 {
			return false
		}
	}
	return true
}

func TestHiddenSimple(t *testing.T) {
	cases := []struct {
		name   string
		values []float64
		window int
		want   []float64
	}{
		{"all windows", []float64{1, 2, 3, 4, 5, 6}, 3, []float64{2, 3, 4, 5}},
		{"window equals length", []float64{2, 4, 6}, 3, []float64{4}},
		{"window one", []float64{7, 8}, 1, []float64{7, 8}},
		{"window too large", []float64{1, 2}, 3, nil},
		{"window zero", []float64{1, 2}, 0, nil},
		{"empty", nil, 1, nil},
	}
	for _, tc := range cases {
		if got := Simple(tc.values, tc.window); !hiddenEqual(got, tc.want) {
			t.Errorf("%s: Simple = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestHiddenExponential(t *testing.T) {
	got := Exponential([]float64{10, 20, 30}, 0.5)
	if !hiddenEqual(got, []float64{10, 15, 22.5}) {
		t.Fatalf("Exponential = %v", got)
	}
	if got := Exponential([]float64{1, 2}, 1); !hiddenEqual(got, []float64{1, 2}) {
		t.Fatalf("alpha 1 = %v", got)
	}
	for _, alpha := range []float64{0, -0.1, 1.1} {
		if got := Exponential([]float64{1, 2}, alpha); got != nil {
			t.Errorf("alpha %v = %v, want nil", alpha, got)
		}
	}
	if got := Exponential(nil, 0.5); got != nil {
		t.Errorf("empty input = %v, want nil", got)
	}
}
