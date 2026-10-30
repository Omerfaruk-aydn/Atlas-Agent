package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSideQuestionCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in       string
		question string
		ok       bool
	}{
		{"/btw what is this?", "what is this?", true},
		{"  /btw\twhat is this?  ", "what is this?", true},
		{"/btw", "", true},
		{"/btw   ", "", true},
		{"/btwx hello", "", false},
		{"/goal ship it", "", false},
		{"please /btw this", "", false},
		{"hello", "", false},
	}
	for _, tt := range tests {
		question, ok := parseSideQuestionCommand(tt.in)
		require.Equal(t, tt.ok, ok, tt.in)
		require.Equal(t, tt.question, question, tt.in)
	}
}

func TestStopSideQuestionMakesAnswerStale(t *testing.T) {
	t.Parallel()

	var m UI
	canceled := false
	m.sideQuestion.cancel = func() { canceled = true }
	gen := m.sideQuestion.gen

	m.stopSideQuestion()

	require.True(t, canceled)
	require.NotEqual(t, gen, m.sideQuestion.gen)
	require.Nil(t, m.sideQuestion.cancel)
}
