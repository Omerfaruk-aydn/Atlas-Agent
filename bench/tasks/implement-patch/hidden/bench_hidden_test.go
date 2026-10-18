package patch

import (
	"errors"
	"testing"
)

func TestHiddenApply(t *testing.T) {
	cases := []struct {
		name     string
		original string
		diff     string
		want     string
	}{
		{
			name:     "replace one line",
			original: "a\nb\nc\n",
			diff:     "@@ -1,3 +1,3 @@\n a\n-b\n+B\n c\n",
			want:     "a\nB\nc\n",
		},
		{
			name:     "file headers are ignored",
			original: "a\nb\n",
			diff:     "diff --git a/f b/f\nindex 1..2 100644\n--- a/f\n+++ b/f\n@@ -2 +2 @@ func name\n-b\n+B\n",
			want:     "a\nB\n",
		},
		{
			name:     "insert at the top",
			original: "a\nb\n",
			diff:     "@@ -0,0 +1,2 @@\n+x\n+y\n",
			want:     "x\ny\na\nb\n",
		},
		{
			name:     "insert at the end",
			original: "a\nb\nc\n",
			diff:     "@@ -3,0 +4,1 @@\n+d\n",
			want:     "a\nb\nc\nd\n",
		},
		{
			name:     "insert in the middle",
			original: "a\nb\nc\n",
			diff:     "@@ -1,0 +2,1 @@\n+between\n",
			want:     "a\nbetween\nb\nc\n",
		},
		{
			name:     "delete lines",
			original: "a\nb\nc\nd\n",
			diff:     "@@ -2,2 +2,0 @@\n-b\n-c\n",
			want:     "a\nd\n",
		},
		{
			name:     "delete everything",
			original: "a\n",
			diff:     "@@ -1 +0,0 @@\n-a\n",
			want:     "",
		},
		{
			name:     "two hunks use original line numbers",
			original: "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\n",
			diff:     "@@ -2,2 +2,3 @@\n l2\n-l3\n+L3\n+L3b\n@@ -6,2 +7,1 @@\n l6\n-l7\n",
			want:     "l1\nl2\nL3\nL3b\nl4\nl5\nl6\nl8\n",
		},
		{
			name:     "drifted file uses an offset",
			original: "p\nq\na\nb\nc\n",
			diff:     "@@ -1,3 +1,3 @@\n a\n-b\n+B\n c\n",
			want:     "p\nq\na\nB\nc\n",
		},
		{
			name:     "equally near candidates resolve to the earlier one",
			original: "a\nx\na\n",
			diff:     "@@ -2 +2,0 @@\n-a\n",
			want:     "x\na\n",
		},
		{
			name:     "offset search can move up",
			original: "a\nb\nc\nd\n",
			diff:     "@@ -4,2 +4,2 @@\n b\n-c\n+C\n",
			want:     "a\nb\nC\nd\n",
		},
		{
			name:     "text without a final newline keeps none",
			original: "a\nb",
			diff:     "@@ -2 +2 @@\n-b\n+B\n\\ No newline at end of file\n",
			want:     "a\nB",
		},
		{
			name:     "final newline is kept",
			original: "a\nb\n",
			diff:     "@@ -1 +1 @@\n-a\n+A\n",
			want:     "A\nb\n",
		},
		{
			name:     "empty original gets a final newline",
			original: "",
			diff:     "@@ -0,0 +1,2 @@\n+one\n+two\n",
			want:     "one\ntwo\n",
		},
		{
			name:     "blank body line is an empty context line",
			original: "a\n\nb\n",
			diff:     "@@ -1,3 +1,3 @@\n a\n\n-b\n+B\n",
			want:     "a\n\nB\n",
		},
		{
			name:     "no hunks leaves the text alone",
			original: "a\nb",
			diff:     "--- a\n+++ b\n",
			want:     "a\nb",
		},
		{
			name:     "empty diff leaves the text alone",
			original: "a\n",
			diff:     "",
			want:     "a\n",
		},
		{
			name:     "second hunk is placed after the first",
			original: "x\ny\nx\ny\n",
			diff:     "@@ -1,2 +1,2 @@\n-x\n+X\n y\n@@ -1,2 +1,2 @@\n-x\n+Z\n y\n",
			want:     "X\ny\nZ\ny\n",
		},
	}
	for _, tc := range cases {
		got, err := Apply(tc.original, tc.diff)
		if err != nil {
			t.Errorf("%s: unexpected error %v", tc.name, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: Apply = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestHiddenApplyConflicts(t *testing.T) {
	cases := []struct {
		name     string
		original string
		diff     string
	}{
		{"context never matches", "a\nb\nc\n", "@@ -1,3 +1,3 @@\n a\n-X\n+B\n c\n"},
		{"deleted line differs", "a\nb\n", "@@ -2 +2,0 @@\n-nope\n"},
		{"matching is exact", "a\n b\n", "@@ -2 +2 @@\n-b\n+B\n"},
		{"hunk may not go back before the previous one", "l1\nl2\nl3\nl4\nl5\n", "@@ -4 +4,0 @@\n-l4\n@@ -1 +1,0 @@\n-l1\n"},
		{"hunks may not overlap", "a\nb\nc\n", "@@ -1,2 +1,0 @@\n-a\n-b\n@@ -2,2 +2,0 @@\n-b\n-c\n"},
	}
	for _, tc := range cases {
		got, err := Apply(tc.original, tc.diff)
		if !errors.Is(err, ErrConflict) {
			t.Errorf("%s: Apply = %q, %v; want ErrConflict", tc.name, got, err)
		}
	}
}

func TestHiddenApplyMalformed(t *testing.T) {
	cases := []struct {
		name string
		diff string
	}{
		{"old count too small", "@@ -1,1 +1,2 @@\n a\n-b\n+B\n"},
		{"new count too large", "@@ -1,2 +1,3 @@\n a\n-b\n+B\n"},
		{"unknown body line", "@@ -1 +1 @@\n-a\n*b\n"},
		{"broken header", "@@ -x +1 @@\n a\n"},
		{"header missing closing marker", "@@ -1 +1\n a\n"},
		{"default count is one", "@@ -1 +1 @@\n a\n b\n"},
	}
	for _, tc := range cases {
		got, err := Apply("a\nb\n", tc.diff)
		if !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: Apply = %q, %v; want ErrMalformed", tc.name, got, err)
		}
	}
}
