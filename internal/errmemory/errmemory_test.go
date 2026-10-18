package errmemory

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSignatureIgnoresPathsNumbersAndTimes(t *testing.T) {
	t.Parallel()
	a, line, ok := Signature("building...\n/home/u/app/main.go:12:3: undefined: helper\n")
	require.True(t, ok)
	require.Equal(t, "/home/u/app/main.go:12:3: undefined: helper", line)
	b, _, ok := Signature(`C:\work\app\main.go:40:9: undefined: helper`)
	require.True(t, ok)
	require.Equal(t, a, b)
	require.Equal(t, "<path>: undefined: helper", a)

	c, _, ok := Signature("2026-10-16T10:00:01Z ERROR: connection refused to db at 0xc000123 after 3 tries")
	require.True(t, ok)
	require.Equal(t, "<time> error: connection refused to db at <hex> after <n> tries", c)

	_, _, ok = Signature("all good\n42 passed\n")
	require.False(t, ok)
	_, _, ok = Signature("    at Object.<anonymous> (index.js:1:1)\nmake: *** [all] Error 1\n")
	require.False(t, ok, "stack frames and make's wrapper line are noise")
}

func TestLearnAndLookup(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	_, ok := Lookup(root, "error: x")
	require.False(t, ok)

	require.NoError(t, Learn(root, "error: x", "error: x happened", "make", []string{"a.c: +`int x;`"}, []string{"a.c"}))
	require.NoError(t, Learn(root, "error: x", "error: x happened again", "make", []string{"b.c: -`x--`"}, []string{"b.c"}))
	entry, ok := Lookup(root, "error: x")
	require.True(t, ok)
	require.Equal(t, 2, entry.Fixed)
	require.Equal(t, []string{"b.c: -`x--`"}, entry.Fixes)

	note := Note(entry)
	require.Contains(t, note, "Seen before in this project (fixed 2 times): `error: x happened again` from `make`")
	require.Contains(t, note, "- b.c: -`x--`")

	require.NoError(t, Learn(root, "", "ignored", "make", []string{"fix"}, nil))
	require.NoError(t, Learn(root, "error: y", "y", "make", nil, nil))
	_, ok = Lookup(root, "error: y")
	require.False(t, ok, "a fix without edits is not remembered")
}
