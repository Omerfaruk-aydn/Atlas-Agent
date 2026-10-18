package semver

import (
	"errors"
	"testing"
)

func TestHiddenCompareCore(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.3", "1.2.4", -1},
		{"1.2.4", "1.2.3", 1},
		{"1.2.3", "1.2.3", 0},
		{"1.10.0", "1.9.0", 1},
		{"2.0.0", "10.0.0", -1},
		{"0.0.1", "0.0.0", 1},
		{"v1.2.3", "1.2.3", 0},
		{"v1.2.3", "v1.2.4", -1},
		{"1.2.3+build.1", "1.2.3+build.2", 0},
		{"1.2.3+build", "1.2.3", 0},
	}
	for _, tc := range cases {
		got, err := Compare(tc.a, tc.b)
		if err != nil || got != tc.want {
			t.Errorf("Compare(%q, %q) = %d, %v; want %d", tc.a, tc.b, got, err, tc.want)
		}
	}
}

func TestHiddenComparePrereleaseOrdering(t *testing.T) {
	// The ordering example from the Semantic Versioning 2.0.0 specification.
	ordered := []string{
		"1.0.0-alpha",
		"1.0.0-alpha.1",
		"1.0.0-alpha.beta",
		"1.0.0-beta",
		"1.0.0-beta.2",
		"1.0.0-beta.11",
		"1.0.0-rc.1",
		"1.0.0",
	}
	for i := range ordered {
		for j := range ordered {
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			got, err := Compare(ordered[i], ordered[j])
			if err != nil || got != want {
				t.Errorf("Compare(%q, %q) = %d, %v; want %d", ordered[i], ordered[j], got, err, want)
			}
		}
	}
}

func TestHiddenComparePrereleaseDetails(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0-1", "1.0.0-alpha", -1},
		{"1.0.0-2", "1.0.0-10", -1},
		{"1.0.0-a-b", "1.0.0-a-c", -1},
		{"1.0.0-Alpha", "1.0.0-alpha", -1},
		{"1.0.0-alpha+x", "1.0.0-alpha+y", 0},
		{"1.0.1-alpha", "1.0.0", 1},
		{"1.0.0-0", "1.0.0-0", 0},
	}
	for _, tc := range cases {
		got, err := Compare(tc.a, tc.b)
		if err != nil || got != tc.want {
			t.Errorf("Compare(%q, %q) = %d, %v; want %d", tc.a, tc.b, got, err, tc.want)
		}
	}
}

func TestHiddenCompareInvalid(t *testing.T) {
	for _, bad := range []string{
		"",
		"1",
		"1.2",
		"1.2.3.4",
		"a.b.c",
		"1.2.x",
		"01.2.3",
		"1.02.3",
		"1.2.03",
		"1.2.3-",
		"1.2.3-alpha..1",
		"1.2.3-01",
		"1.2.3-al_pha",
		"1.2.3+",
		"1.2.3+a..b",
		"1.2.3+bad_char",
		"vv1.2.3",
		"V1.2.3",
		" 1.2.3",
		"-1.2.3",
	} {
		if _, err := Compare(bad, "1.0.0"); !errors.Is(err, ErrInvalid) {
			t.Errorf("Compare(%q, ...) error = %v, want ErrInvalid", bad, err)
		}
		if _, err := Compare("1.0.0", bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("Compare(..., %q) error = %v, want ErrInvalid", bad, err)
		}
	}
}
