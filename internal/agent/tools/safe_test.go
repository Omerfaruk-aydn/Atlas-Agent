package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The permission shortcut used to prefix-match safeCommands, so these
// mutating forms ran without a prompt even in manual mode.
func TestSafeReadOnlyCommand(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"ls -la":                      true,
		"pwd":                         true,
		"echo hello":                  true,
		"git status":                  true,
		"git diff HEAD~1":             true,
		"git log --oneline -5":        true,
		"git branch":                  true,
		"git branch -a":               true,
		"git branch --show-current":   true,
		"git tag -l":                  true,
		"git remote -v":               true,
		"git config --get user.name":  true,
		"hostname":                    true,
		"date":                        true,
		"echo x > ~/.bashrc":          false,
		"echo x >> notes":             false,
		"cat < /etc/passwd":           false,
		"ls\nrm -rf x":                false,
		"ls & rm x":                   false,
		"kill -9 1":                   false,
		"killall node":                false,
		"timeout 5 rm x":              false,
		"nohup ./run.sh":              false,
		"nice rm x":                   false,
		"time rm x":                   false,
		"env rm -rf x":                false,
		"set -e":                      false,
		"git branch -D main":          false,
		"git branch newbranch":        false,
		"git tag v1":                  false,
		"git remote add o url":        false,
		"git remote set-url origin u": false,
		"git diff --output=out.txt":   false,
		"git diff --ext-diff":         false,
		"git grep -O less foo":        false,
		"date -s 2020-01-01":          false,
		"date --set=2020-01-01":       false,
		"hostname newname":            false,
		"hostname -F file":            false,
		"ls; rm x":                    false,
		"ls | xargs rm":               false,
		"ls $(rm x)":                  false,
		"":                            false,
		"lsattr -R /":                 false,
		"git statusx":                 false,
	}
	for command, want := range tests {
		t.Run(command, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, safeReadOnlyCommand(command), "safeReadOnlyCommand(%q)", command)
		})
	}
}

func TestContainsCommandChaining(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"plain ls", "ls -la", false},
		{"plain echo", "echo hello world", false},
		{"plain pwd", "pwd", false},
		{"plain git status", "git status", false},
		{"ls with redirect", "ls > /tmp/out", false},
		{"ls with pipe", "ls | grep foo", true},
		{"ls with double ampersand", "ls && echo done", true},
		{"ls with semicolon", "ls; echo done", true},
		{"ls with pipe pipe", "ls || echo fail", true},
		{"ls with backticks", "ls `echo foo`", true},
		{"ls with subshell", "ls $(echo foo)", true},
		{"ls with background ampersand", "ls & echo done", false},
		{"rm -rf with && ls (rm first)", "rm -rf / && ls", true},
		{"redirect with ampersand gt", "ls &> /dev/null", false},
		{"redirect with gt ampersand", "ls >& /dev/null", false},
		{"simple kill", "kill 1234", false},
		{"kill with pipe", "kill 1234 | echo foo", true},
		{"git log", "git log --oneline", false},
		{"git log with pipe", "git log | head", true},
		{"empty string", "", false},
		{"dollar sign in argument", "echo $HOME", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := containsCommandChaining(tt.input)
			assert.Equal(t, tt.expected, got, "containsCommandChaining(%q)", tt.input)
		})
	}
}
