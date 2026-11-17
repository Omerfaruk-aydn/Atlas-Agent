package tools

import (
	"encoding/json"
	"runtime"
	"slices"
	"strings"
)

// safeCommands lists commands that only observe state. A command is safe
// only when it starts with one of these AND passes safeReadOnlyCommand, which
// rejects redirections, chaining and the arguments that turn an observation
// into a mutation (date -s, git diff --output, ...).
//
// Commands that run other programs (env, nice, nohup, time, timeout) or that
// change state (kill, killall, set, unset) are deliberately absent: they used
// to be here and let `kill -9 1` or `timeout 5 rm x` skip the prompt.
var safeCommands = []string{
	// Bash builtins and core utils
	"cal",
	"date",
	"df",
	"du",
	"echo",
	"free",
	"groups",
	"hostname",
	"id",
	"ls",
	"printenv",
	"ps",
	"pwd",
	"top",
	"type",
	"uname",
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

// gitListingArgs are the only arguments under which `git branch`, `git tag`
// and `git remote` merely list. Anything else (a branch name, -D, -d, add,
// set-url, ...) mutates the repository.
var gitListingArgs = map[string]map[string]bool{
	"git branch": {
		"-a": true, "--all": true, "-r": true, "--remotes": true, "-v": true, "-vv": true,
		"--verbose": true, "--list": true, "-l": true, "--show-current": true,
	},
	"git tag":    {"-l": true, "--list": true, "-n": true},
	"git remote": {"-v": true, "--verbose": true},
}

// unsafeArgs are arguments that make an otherwise read-only command write a
// file, run an external program or change system state.
var unsafeArgs = map[string][]string{
	"date":     {"-s", "--set"},
	"hostname": {"-b", "-f", "-F", "--file", "--boot"},
	"git":      {"--output", "--ext-diff", "--textconv", "--open-files-in-pager", "-O", "--exec-path"},
}

// safeReadOnlyCommand reports whether command is a single observation
// command whose arguments cannot mutate anything. It is intentionally
// conservative: when unsure it answers false and the user is asked.
func safeReadOnlyCommand(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" || containsCommandChaining(command) || strings.ContainsAny(command, "><\n\r&") {
		return false
	}
	lower := strings.ToLower(command)
	var matched string
	for _, safe := range safeCommands {
		if lower == safe || strings.HasPrefix(lower, safe+" ") {
			if len(safe) > len(matched) {
				matched = safe
			}
		}
	}
	if matched == "" {
		return false
	}
	args := strings.Fields(command)[len(strings.Fields(matched)):]

	if allowed, ok := gitListingArgs[matched]; ok {
		for _, a := range args {
			if !allowed[a] {
				return false
			}
		}
	}
	if matched == "hostname" && len(args) > 0 {
		for _, a := range args {
			if !strings.HasPrefix(a, "-") || slices.Contains(unsafeArgs["hostname"], a) {
				return false
			}
		}
	}
	tool, _, _ := strings.Cut(matched, " ")
	for _, a := range args {
		for _, bad := range unsafeArgs[tool] {
			if a == bad || strings.HasPrefix(a, bad+"=") {
				return false
			}
		}
	}
	return true
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
	if !safeReadOnlyCommand(command) {
		return false
	}
	// The effect tracker is stricter than the permission shortcut: even
	// configuration reads are treated as possible changes.
	return !strings.HasPrefix(command, "git config")
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
