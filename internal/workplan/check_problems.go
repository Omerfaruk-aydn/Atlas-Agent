package workplan

import (
	"regexp"
	"strings"
)

// A check is a command that exits 0 when the work is right. Two kinds of
// entries look like checks but can never pass: a command that starts a
// server or watcher, which never exits, and a sentence describing a manual
// step ("npm run build; browser desktop/mobile inspection"). Demanding them
// as the contract left the gate unsatisfiable, so they are reported to the
// agent to fix and are not demanded. Browser test suites are reported the
// same way: a web UI is checked with the browser tool.

var (
	serverCommand = regexp.MustCompile(`(?i)(^|[;&|]\s*|\s)((npm|pnpm|yarn|bun)\s+(run\s+)?(dev|start|serve|preview|watch)\b|vite(\s+--|\s*$|\s+(dev|serve|preview)\b)|next\s+(dev|start)\b|nodemon\b|http-server\b|python3?\s+-m\s+http\.server|--watch\b)`)
	segmentSplit  = regexp.MustCompile(`;|&&|\|\|`)
	// browserSuite is a browser test framework. Atlas checks web UIs with
	// its browser tool instead: a suite of pixel probes run as a contract
	// check took half an hour per run and had to be rerun after every
	// change, which is how one run spent hours re-running tests.
	browserSuite = regexp.MustCompile(`(?i)\b(playwright|puppeteer|cypress|selenium|webdriverio|wdio)\b|\b(npm|pnpm|yarn|bun)\s+(run\s+)?(test:)?e2e\b`)
)

// proseStarts are words a sentence about a manual step starts with but no
// command does.
var proseStarts = map[string]bool{
	"inspect": true, "inspection": true, "manual": true, "manually": true, "visual": true, "visually": true,
	"browser": true, "review": true, "look": true, "ensure": true, "confirm": true, "verify": true,
	"check": true, "screenshot": true, "screenshots": true, "capture": true, "captures": true, "observe": true,
}

// CheckProblem returns why command cannot work as a check, or "".
func CheckProblem(command string) string {
	command = strings.TrimSpace(command)
	switch {
	case command == "":
		return "is empty"
	case serverCommand.MatchString(command):
		return "starts a server or watcher that never exits, so it can never pass; check against a running server with a command that exits (a test runner, curl), or leave the server out of the contract"
	case browserSuite.MatchString(command):
		return "runs a browser test suite; keep the build, typecheck and unit tests in the contract and check the page with the browser tool instead (open it, go through each flow, read the console, take desktop and phone screenshots)"
	case isProse(command):
		return "describes a manual step rather than running a command; turn it into a command that exits 0 when the step passes (for example an end-to-end test), or remove it"
	}
	return ""
}

// isProse reports whether a part of command reads as a sentence.
func isProse(command string) bool {
	for _, segment := range segmentSplit.Split(command, -1) {
		words := strings.Fields(segment)
		if len(words) > 0 && proseStarts[strings.ToLower(words[0])] {
			return true
		}
	}
	return false
}

// CheckProblems lists the plan's contract checks and feature checks that
// cannot pass, as "`command` reason" lines.
func (p *Plan) CheckProblems() []string {
	if p == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	add := func(command string) {
		if seen[command] {
			return
		}
		seen[command] = true
		if problem := CheckProblem(command); problem != "" {
			out = append(out, "`"+strings.TrimSpace(command)+"` "+problem)
		}
	}
	for _, check := range p.Verify {
		add(check.Command)
	}
	for _, feature := range p.Features {
		if feature.Verify != "" {
			add(feature.Verify)
		}
	}
	return out
}
