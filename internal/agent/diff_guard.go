package agent

import (
	"fmt"
	"log/slog"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workplan"
)

// The diff guard looks at what a request changed before the turn may end.
// Two kinds of finding go back to the agent, each once:
//
//   - Weakened checks. An agent stuck on a failing check can make it pass
//     by loosening the check instead of fixing the code: deleting
//     assertions or whole tests, adding skip or focus markers, silencing a
//     linter, lowering a coverage threshold, or editing the plan's verify
//     commands. Each can be legitimate, so the agent is asked to undo it or
//     justify it, and the evaluator sees the list.
//   - Leftovers: debug statements, conflict markers, new TODOs,
//     commented-out code, likely secrets and large new files.
//
// The analysis is textual and language-agnostic, built on patterns common
// to many ecosystems. It only reads files captured by the edit baseline, so
// it can miss a change but never invents one.

// diffGuardMaxRounds bounds the send-backs for diff findings per request.
const diffGuardMaxRounds = 2

// diffGuardMaxShown bounds the findings listed in one send-back.
const diffGuardMaxShown = 20

var (
	testPathPattern = regexp.MustCompile(`(?i)(^|/)(tests?|__tests__|specs?|e2e|integration[-_]?tests?)/|_test\.[a-z]+$|\.(test|spec)\.[a-z]+$|(^|/)test_[^/]+\.py$|tests?\.(java|kt|cs|swift|scala)$|_spec\.rb$`)
	testCasePattern = regexp.MustCompile(`(?m)^\s*(?:func\s+Test\w*\s*\(|(?:async\s+)?def\s+test\w*\s*\(|(?:it|test|specify)\s*\(\s*['"` + "`" + `]|@Test\b|#\[test\]|#\[tokio::test|TEST(?:_F|_P)?\s*\(|\[(?:Fact|Theory|Test|TestMethod|TestCase)\b)`)
	assertPattern   = regexp.MustCompile(`\b(?:assert\w*|expect(?:That|s)?|require\.\w+|t\.(?:Errorf?|Fatalf?|Fail(?:Now)?)|XCTAssert\w*|EXPECT_[A-Z_]+|ASSERT_[A-Z_]+|should(?:\.\w+)?|must(?:Equal|Be)\w*)\b\s*[(!.]|^\s*assert\s`)
	skipPattern     = regexp.MustCompile(`\bt\.Skip(?:f|Now)?\s*\(|\b(?:it|test|describe|context|suite)\.(?:skip|only|todo)\s*\(|(?:^|[^\w.])[xf](?:it|describe|test|context)\s*\(|@pytest\.mark\.(?:skip|skipif|xfail)\b|\bpytest\.skip\s*\(|\bunittest\.skip\w*|@Disabled\b|@Ignore\b|#\[ignore\]|\[(?:Ignore|Skip)\b|\bSkip\s*=\s*"|\bGTEST_SKIP\b|\bDISABLED_\w+|\bskip\s*\(\s*\)`)
	onlyPattern     = regexp.MustCompile(`\b(?:it|test|describe|context|suite)\.only\s*\(|(?:^|[^\w.])f(?:it|describe)\s*\(`)
	suppressPattern = regexp.MustCompile(`eslint-disable|@ts-ignore|@ts-nocheck|@ts-expect-error|#\s*noqa\b|#\s*type:\s*ignore|//\s*nolint|#\s*pylint:\s*disable|#!?\[allow\(|@SuppressWarnings|#pragma\s+warning\s+disable|NOSONAR|rubocop:disable|pragma:\s*no\s*cover|istanbul\s+ignore|c8\s+ignore|v8\s+ignore|@phpstan-ignore|phpcs:ignore|swiftlint:disable|//\s*lint:ignore`)
	coveragePattern = regexp.MustCompile(`(?i)\b(fail[_-]?under|cov[_-]?fail[_-]?under|coverage[_-]?threshold|min[_-]?coverage|minimum[_-]?coverage|branches|functions|lines|statements)\b["']?\s*[:=]\s*["']?(\d+(?:\.\d+)?)`)
	coverageFile    = regexp.MustCompile(`(?i)(jest|vitest|nyc|karma|c8|coverage|codecov|pytest|tox|setup\.cfg|pyproject|package\.json|makefile|\.ya?ml$|\.coveragerc)`)
	conflictPattern = regexp.MustCompile(`^(<{7}|>{7}) \S`)
	todoPattern     = regexp.MustCompile(`\b(TODO|FIXME|XXX|HACK)\b`)
	commentLine     = regexp.MustCompile(`^\s*(//|#(?:\s|$)|--\s)`)
	codeish         = regexp.MustCompile(`[;{}()=]|\breturn\b|\bif\b`)
)

// debugPatterns are debug statements by file extension. A pattern is only
// reported when the file had no match before the request, so a project
// that logs this way on purpose is left alone.
var debugPatterns = map[string]*regexp.Regexp{
	".js":     regexp.MustCompile(`\bconsole\.(debug|trace)\s*\(|^\s*debugger\s*;?\s*$`),
	".py":     regexp.MustCompile(`\b(i?pdb)\.set_trace\s*\(|^\s*breakpoint\s*\(\s*\)|\bic\s*\(`),
	".rb":     regexp.MustCompile(`\bbinding\.(pry|irb)\b|\bbyebug\b`),
	".go":     regexp.MustCompile(`\bspew\.Dump\s*\(|\bfmt\.Print(?:ln|f)?\s*\(\s*"(?i:debug)`),
	".rs":     regexp.MustCompile(`\bdbg!\s*\(`),
	".php":    regexp.MustCompile(`\b(var_dump|print_r|dd|dump)\s*\(`),
	".java":   regexp.MustCompile(`\.printStackTrace\s*\(\s*\)`),
	".dart":   regexp.MustCompile(`\bdebugPrint\s*\(`),
	".swift":  regexp.MustCompile(`\bdebugPrint\s*\(|\bdump\s*\(`),
	".ex":     regexp.MustCompile(`\bIO\.inspect\b|\bIEx\.pry\b`),
	".c":      regexp.MustCompile(`\bfprintf\s*\(\s*stderr\s*,\s*"(?i:debug)`),
	".kotlin": regexp.MustCompile(`\.printStackTrace\s*\(\s*\)`),
}

// jsLogPattern is console output, which is ordinary in scripts and
// command-line tools; it is only reported in an existing file that did not
// print to the console before.
var jsLogPattern = regexp.MustCompile(`\bconsole\.(log|dir)\s*\(`)

var debugAliases = map[string]string{
	".jsx": ".js", ".ts": ".js", ".tsx": ".js", ".mjs": ".js", ".cjs": ".js", ".vue": ".js", ".svelte": ".js",
	".exs": ".ex", ".kt": ".kotlin", ".cpp": ".c", ".cc": ".c", ".h": ".c", ".hpp": ".c",
}

// secretPatterns are high-confidence credential shapes. The value itself is
// never repeated in a finding.
var secretPatterns = []struct {
	name    string
	pattern *regexp.Regexp
}{
	{"AWS access key", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{"private key", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{"GitHub token", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}\b`)},
	{"Slack token", regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`)},
	{"API key", regexp.MustCompile(`\bsk-(?:proj-|ant-)?[A-Za-z0-9_-]{20,}\b`)},
	{"Google API key", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)},
	{"hard-coded credential", regexp.MustCompile(`(?i)\b(api[_-]?key|secret|passw(?:or)?d|access[_-]?token|auth[_-]?token|private[_-]?key|client[_-]?secret)\b["']?\s*[:=]\s*["']([^"'\s]{12,})["']`)},
}

var secretPlaceholder = regexp.MustCompile(`(?i)example|placeholder|changeme|change[_-]me|xxxx|your[_-]|<|\$\{|\{\{|process\.env|os\.environ|getenv|dummy|fake|sample|redacted|\*\*\*`)

// largeNewFile is the size above which a file created by the request is
// flagged: generated output, a build artifact or a dataset is rarely meant
// to be part of the change.
const largeNewFile = 1 << 20

func isTestFile(path, before string) bool {
	return testPathPattern.MatchString(path) || len(testCasePattern.FindAllStringIndex(before, 1)) > 0
}

func countMatches(pattern *regexp.Regexp, text string) int {
	n := 0
	for line := range strings.SplitSeq(text, "\n") {
		n += len(pattern.FindAllStringIndex(line, -1))
	}
	return n
}

// addedLines returns the lines of after that are not in before, counting
// duplicates, with their 1-based line numbers.
func addedLines(before, after string) []numberedLine {
	have := map[string]int{}
	for line := range strings.SplitSeq(before, "\n") {
		have[strings.TrimRight(line, "\r")]++
	}
	var out []numberedLine
	for i, line := range strings.Split(after, "\n") {
		line = strings.TrimRight(line, "\r")
		if have[line] > 0 {
			have[line]--
			continue
		}
		out = append(out, numberedLine{n: i + 1, text: line})
	}
	return out
}

type numberedLine struct {
	n    int
	text string
}

// weakeningFindings reports how a change loosens the checks in one file.
func weakeningFindings(c fileChange) []string {
	if !c.existed {
		return nil // A new file adds checks; it cannot weaken existing ones.
	}
	var out []string
	if !c.exists {
		if isTestFile(c.path, c.before) {
			out = append(out, fmt.Sprintf("%s: test file deleted (%d test cases, %d assertions)", c.path,
				countMatches(testCasePattern, c.before), countMatches(assertPattern, c.before)))
		}
		return out
	}
	if isTestFile(c.path, c.before) {
		if was, now := countMatches(testCasePattern, c.before), countMatches(testCasePattern, c.after); now < was {
			out = append(out, fmt.Sprintf("%s: test cases went from %d to %d", c.path, was, now))
		}
		if was, now := countMatches(assertPattern, c.before), countMatches(assertPattern, c.after); now < was {
			out = append(out, fmt.Sprintf("%s: assertions went from %d to %d", c.path, was, now))
		}
		if was, now := countMatches(skipPattern, c.before), countMatches(skipPattern, c.after); now > was {
			out = append(out, fmt.Sprintf("%s: %d new skip, focus or disabled-test marker(s)%s", c.path, now-was, firstAdded(c, skipPattern)))
		}
	} else if was, now := countMatches(onlyPattern, c.before), countMatches(onlyPattern, c.after); now > was {
		out = append(out, fmt.Sprintf("%s: %d new focus marker(s) that make other tests not run%s", c.path, now-was, firstAdded(c, onlyPattern)))
	}
	if was, now := countMatches(suppressPattern, c.before), countMatches(suppressPattern, c.after); now > was {
		out = append(out, fmt.Sprintf("%s: %d new lint or type-check suppression(s)%s", c.path, now-was, firstAdded(c, suppressPattern)))
	}
	if coverageFile.MatchString(path.Base(c.path)) || coverageFile.MatchString(c.path) {
		for key, was := range coverageLevels(c.before) {
			if now, ok := coverageLevels(c.after)[key]; ok && now < was {
				out = append(out, fmt.Sprintf("%s: coverage threshold %s lowered from %g to %g", c.path, key, was, now))
			}
		}
	}
	return out
}

// firstAdded names the line of the first added match, for orientation.
func firstAdded(c fileChange, pattern *regexp.Regexp) string {
	for _, line := range addedLines(c.before, c.after) {
		if pattern.MatchString(line.text) {
			return fmt.Sprintf(" (first at line %d)", line.n)
		}
	}
	return ""
}

// coverageLevels reads coverage thresholds by key, keeping the highest
// value per key so reordering a file does not look like a change.
func coverageLevels(text string) map[string]float64 {
	levels := map[string]float64{}
	for _, m := range coveragePattern.FindAllStringSubmatch(text, -1) {
		value, err := strconv.ParseFloat(m[2], 64)
		if err != nil || value > 100 {
			continue
		}
		key := strings.ToLower(strings.NewReplacer("-", "_").Replace(m[1]))
		levels[key] = max(levels[key], value)
	}
	return levels
}

// hygieneFindings reports leftovers in the lines a change added to a file.
func hygieneFindings(c fileChange) []string {
	if !c.exists {
		return nil
	}
	if !c.existed && c.size > largeNewFile {
		return []string{fmt.Sprintf("%s: new file of %d KiB; make sure generated output, build artifacts or data are not meant to be ignored instead", c.path, c.size>>10)}
	}
	if strings.IndexByte(c.after[:min(len(c.after), 8192)], 0) >= 0 {
		return nil // Binary content has no lines to judge.
	}
	added := addedLines(c.before, c.after)
	var out []string
	ext := strings.ToLower(path.Ext(c.path))
	if alias, ok := debugAliases[ext]; ok {
		ext = alias
	}
	debug := debugPatterns[ext]
	debugBefore := debug != nil && countMatches(debug, c.before) > 0
	testFile := isTestFile(c.path, c.before)
	jsLog := ext == ".js" && c.existed && countMatches(jsLogPattern, c.before) == 0
	prose := ext == ".md" || ext == ".txt" || ext == ".rst" || ext == ".adoc"
	var todos []int
	commentRun, codeRun, lastLine := 0, 0, 0
	flushComments := func() {
		if commentRun >= 4 && codeRun >= 3 {
			out = append(out, fmt.Sprintf("%s:%d: %d lines of commented-out code; delete it (version control keeps history) or explain why it stays", c.path, lastLine-commentRun+1, commentRun))
		}
		commentRun, codeRun = 0, 0
	}
	for _, line := range added {
		text := line.text
		if conflictPattern.MatchString(text) {
			out = append(out, fmt.Sprintf("%s:%d: merge conflict marker", c.path, line.n))
		}
		for _, secret := range secretPatterns {
			m := secret.pattern.FindStringSubmatch(text)
			if m == nil || secretPlaceholder.MatchString(m[len(m)-1]) || secretPlaceholder.MatchString(text) && len(m) > 1 {
				continue
			}
			out = append(out, fmt.Sprintf("%s:%d: looks like a %s; load it from configuration or the environment instead of the source", c.path, line.n, secret.name))
			break
		}
		if !testFile && (debug != nil && !debugBefore && debug.MatchString(text) || jsLog && jsLogPattern.MatchString(text)) {
			out = append(out, fmt.Sprintf("%s:%d: debug statement `%s`", c.path, line.n, clipLine(text)))
		}
		if !prose && todoPattern.MatchString(text) {
			todos = append(todos, line.n)
		}
		if !prose && commentLine.MatchString(text) && line.n == lastLine+1 {
			commentRun++
			if codeish.MatchString(text) {
				codeRun++
			}
		} else {
			flushComments()
			if !prose && commentLine.MatchString(text) {
				commentRun = 1
				if codeish.MatchString(text) {
					codeRun = 1
				}
			}
		}
		lastLine = line.n
	}
	flushComments()
	if len(todos) > 0 {
		out = append(out, fmt.Sprintf("%s: %d new TODO/FIXME marker(s) at line %s; finish the work or list what is left in your report", c.path, len(todos), joinInts(todos[:min(len(todos), 5)])))
	}
	return out
}

func joinInts(values []int) string {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		parts = append(parts, strconv.Itoa(v))
	}
	return strings.Join(parts, ", ")
}

func clipLine(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 80 {
		return s[:80] + "…"
	}
	return s
}

// planWeakening compares the plan before the request with the current one.
func planWeakening(c fileChange) []string {
	if !c.existed || !strings.HasSuffix(c.path, workplan.Dir+"/"+workplan.PlanFile) {
		return nil
	}
	before, err := workplan.Parse([]byte(c.before))
	if err != nil {
		return nil
	}
	if !c.exists {
		return []string{c.path + ": the work plan was deleted, and its verification contract with it"}
	}
	after, err := workplan.Parse([]byte(c.after))
	if err != nil {
		return nil // The contract gate reports an unreadable plan itself.
	}
	var out []string
	for _, finding := range workplan.Weakened(before, after) {
		out = append(out, c.path+": "+finding)
	}
	return out
}

// diffFindings runs the enabled checks over the captured changes.
func (a *sessionAgent) diffFindings(ledger *evaluatorLedger) (weakened, leftovers []string) {
	opts := a.harnessOptions()
	for _, c := range a.baselineChanges(ledger) {
		if opts.CheckGuardEnabled() {
			weakened = append(weakened, weakeningFindings(c)...)
			weakened = append(weakened, planWeakening(c)...)
		}
		if opts.DiffHygieneEnabled() && !workplan.IsPlanFile(c.path) {
			leftovers = append(leftovers, hygieneFindings(c)...)
		}
	}
	return weakened, leftovers
}

// reviewDiff sends the work back once for new diff findings. All findings
// raised in the request are kept for the evaluator. The caller holds
// ledger.mu.
func (a *sessionAgent) reviewDiff(sessionID string, ledger *evaluatorLedger) (evaluatorDecision, bool) {
	if a.workingDir == "" || len(ledger.baselines) == 0 {
		return evaluatorDecision{}, false
	}
	weakened, leftovers := a.diffFindings(ledger)
	if ledger.diffRaised == nil {
		ledger.diffRaised = map[string]bool{}
	}
	fresh := func(findings []string) []string {
		var out []string
		for _, f := range findings {
			if !ledger.diffRaised[f] {
				out = append(out, f)
			}
		}
		return out
	}
	newWeakened, newLeftovers := fresh(weakened), fresh(leftovers)
	ledger.diffNotes = append(weakened, leftovers...)
	if len(newWeakened)+len(newLeftovers) == 0 || ledger.diffRounds >= diffGuardMaxRounds {
		return evaluatorDecision{}, false
	}
	for _, f := range append(newWeakened, newLeftovers...) {
		ledger.diffRaised[f] = true
	}
	ledger.diffRounds++
	if len(newWeakened) > 0 {
		a.recordHarness(harnessmetrics.ChecksWeakened, sessionID, map[string]string{"findings": fmt.Sprint(len(newWeakened))})
	}
	if len(newLeftovers) > 0 {
		a.recordHarness(harnessmetrics.Leftovers, sessionID, map[string]string{"findings": fmt.Sprint(len(newLeftovers))})
	}
	slog.Info("Diff guard sent the turn back", "session_id", sessionID, "weakened", len(newWeakened), "leftovers", len(newLeftovers))
	return evaluatorDecision{Continue: diffGuardPrompt(newWeakened, newLeftovers)}, true
}

func diffGuardPrompt(weakened, leftovers []string) string {
	var b strings.Builder
	b.WriteString("Before this work counts as finished, look at these in your change.\n")
	list := func(title string, findings []string) {
		if len(findings) == 0 {
			return
		}
		b.WriteString("\n" + title + "\n")
		for _, f := range findings[:min(len(findings), diffGuardMaxShown)] {
			b.WriteString("- " + f + "\n")
		}
		if hidden := len(findings) - diffGuardMaxShown; hidden > 0 {
			fmt.Fprintf(&b, "- … %d more\n", hidden)
		}
	}
	list("Checks that got weaker:", weakened)
	list("Left in the change:", leftovers)
	b.WriteString("\nFor each one, undo it, or, if it is right for the project, say why in your final report (a test that tested the wrong thing, " +
		"a suppression with its reason in a comment, a threshold the user asked to lower). Never loosen a check to make it pass: fix the code instead. " +
		"Remove debug output and conflict markers, and move any credential to configuration or the environment. Then give your final report.")
	return b.String()
}
