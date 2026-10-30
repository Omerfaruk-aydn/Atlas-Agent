package tools

import (
	"encoding/json"
	"runtime"
	"slices"
	"strings"
)

var safeCommands = []string{
	// Bash builtins and core utils
	"cal",
	"date",
	"df",
	"du",
	"echo",
	"env",
	"free",
	"groups",
	"hostname",
	"id",
	"kill",
	"killall",
	"ls",
	"nice",
	"nohup",
	"printenv",
	"ps",
	"pwd",
	"set",
	"time",
	"timeout",
	"top",
	"type",
	"uname",
	"unset",
	"uptime",
	"whatis",
	"whereis",
	"which",
	"whoami",

	// Git
	"git blame",
	"git branch",
	"git config --get",
	"git config --list",
	"git describe",
	"git diff",
	"git grep",
	"git log",
	"git ls-files",
	"git ls-remote",
	"git remote",
	"git rev-parse",
	"git shortlog",
	"git show",
	"git status",
	"git tag",
}

var chainingMetacharacters = []string{
	";",
	"|",
	"&&",
	"$(",
	"`",
}

// containsCommandChaining reports whether s contains shell metacharacters
// that enable command chaining or substitution.
func containsCommandChaining(s string) bool {
	return slices.ContainsFunc(chainingMetacharacters, func(c string) bool {
		return strings.Contains(s, c)
	})
}

// BashReadOnly reports whether a bash call is a single well-known observation
// command (ls, git status, ...). Anything else, including chained commands,
// is treated as a possible source change by the effect tracker.
func BashReadOnly(input string) bool {
	var p BashParams
	if json.Unmarshal([]byte(input), &p) != nil || p.RunInBackground {
		return false
	}
	command := strings.TrimSpace(p.Command)
	if command == "" || containsCommandChaining(command) || strings.ContainsAny(command, "><\n&") {
		return false
	}
	return slices.ContainsFunc(safeCommands, func(safe string) bool {
		return command == safe || strings.HasPrefix(command, safe+" ")
	}) && !strings.HasPrefix(command, "kill") && !strings.HasPrefix(command, "nohup") &&
		!strings.HasPrefix(command, "timeout") && !strings.HasPrefix(command, "nice") &&
		!strings.HasPrefix(command, "set") && !strings.HasPrefix(command, "unset") &&
		!strings.HasPrefix(command, "git branch") && !strings.HasPrefix(command, "git tag") &&
		!strings.HasPrefix(command, "git remote") && !strings.HasPrefix(command, "git config")
}

func init() {
	if runtime.GOOS == "windows" {
		safeCommands = append(
			safeCommands,
			// Windows-specific commands
			"ipconfig",
			"nslookup",
			"ping",
			"systeminfo",
			"tasklist",
			"where",
		)
	}
}
