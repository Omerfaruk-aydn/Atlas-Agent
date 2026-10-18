package semver

import "testing"

func TestCompareCore(t *testing.T) {
	got, err := Compare("1.2.3", "1.2.4")
	if err != nil || got != -1 {
		t.Fatalf("Compare = %d, %v", got, err)
	}
}
