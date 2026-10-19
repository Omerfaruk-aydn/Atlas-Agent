package tools

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// toolHints are the few words the deferred tool index shows next to a tool
// name. A name alone rarely says when a tool helps ("app_loop", "scenario"),
// and a model never searches for a capability it does not know exists, so the
// hint names the situation the tool is for rather than restating its name.
// Tools without an entry, such as MCP tools, fall back to the first clause of
// their own description.
var toolHints = map[string]string{
	"agentic_fetch":           "research a question across web pages",
	"android":                 "drive an Android device or emulator",
	"anti_pattern_scan":       "Go: swallowed errors, misplaced ctx, panics",
	"api_surface":             "Go: list a package's exported API",
	"app_loop":                "run the real app, exercise the user flow, fix, repeat",
	"atlas_config":            "change Atlas models, roles, settings",
	"atlas_info":              "Atlas runtime state: model, MCP, skills, permissions",
	"atlas_logs":              "read Atlas's own logs",
	"audit_trail":             "history of one function, commit by commit",
	"browser":                 "use a web page like a person: click, type, screenshot",
	"changelog_gen":           "draft a changelog from git history",
	"ci_cd_pipeline_debugger": "check GitHub Actions workflows for risks",
	"cli_bridge":              "run installed CLIs through reviewed profiles",
	"cloud_resource_costs":    "estimate monthly cost from Terraform or Kubernetes",
	"code_metrics":            "Go: complexity and length of functions",
	"computer":                "see and control desktop apps",
	"coverage_report":         "Go: test coverage and untested code",
	"dead_code":               "Go: unreferenced declarations",
	"debugger":                "Go: breakpoints and live variables under Delve",
	"dep_audit":               "Go: vulnerable or outdated dependencies",
	"design_search":           "compare UI styles, fonts, palettes",
	"design_system":           "define and audit a UI design system",
	"docker_build_explain":    "check a Dockerfile's stages for mistakes",
	"download":                "save a URL to a local file",
	"env_var_audit":           "Go: environment variables read and written",
	"facts":                   "remember a fact for the rest of this session",
	"fetch":                   "read a URL as text",
	"generate_docstring":      "Go: stub doc comments for exported names",
	"generate_tests":          "Go: table-driven test skeleton for a function",
	"git_blame":               "who changed a line and why",
	"git_branches":            "list and compare branches",
	"git_commit_split":        "propose how to split changes into commits",
	"git_conflict_resolver":   "summarize merge-conflict regions",
	"git_conventional_commit": "write a conventional commit message",
	"git_diff":                "see exact changes",
	"git_log":                 "read commit history",
	"git_status":              "see changed files",
	"github_pr_view":          "read a GitHub pull request",
	"code_map":                "code, docs, assets: overview, definitions, who uses what",
	"impact_analysis":         "Go: transitive callers of a function",
	"import_graph":            "Go: package dependencies and import cycles",
	"inspect_file":            "look inside archives, SQLite databases, notebooks",
	"integrations":            "see providers, accounts and MCP servers Atlas can reach",
	"isolated_desktop":        "use desktop apps in a disposable sandbox or VM",
	"job_kill":                "stop a background job",
	"job_output":              "read a background job's output",
	"lint_run":                "Go: run the linter, findings by file",
	"list_mcp_resources":      "list MCP server resources",
	"local_app":               "build and run a local web app the user keeps using",
	"log_tail":                "read the end of a log file, filtered",
	"lsp_call_hierarchy":      "who calls a function",
	"lsp_definition":          "jump to a definition",
	"lsp_diagnostics":         "compiler and type errors",
	"lsp_edit_plan":           "apply or recover a previewed refactor",
	"lsp_references":          "find every use of a symbol",
	"lsp_rename":              "rename a symbol everywhere",
	"lsp_rename_file":         "rename a file and fix imports",
	"lsp_replace_symbol":      "replace a whole function or type",
	"lsp_restart":             "restart a stuck language server",
	"lsp_symbols":             "list symbols in a file or project",
	"memory":                  "remember facts across sessions",
	"metric_export":           "Go: list Prometheus metrics",
	"pr_describe":             "draft a pull request description",
	"pre_commit_guard":        "check changes before committing",
	"read_mcp_resource":       "read an MCP server resource",
	"remote_node":             "run commands on a paired machine",
	"remote_support":          "help on someone else's computer",
	"scan_secrets":            "find leaked secrets",
	"scenario":                "validate a scripted web, terminal, desktop or Android flow",
	"screen_mark":             "read regions the user marked on screen",
	"security_scan":           "Go: credentials, weak crypto, disabled TLS",
	"semantic_code_search":    "Go: find a declaration by description",
	"session_search":          "search earlier sessions",
	"skill_manage":            "create or update skills",
	"sourcegraph":             "search code across public repos",
	"team_read":               "read messages from teammates",
	"team_send":               "message a teammate agent",
	"terraform_lint":          "check Terraform for risky settings",
	"test_run":                "Go: run tests, structured failures",
	"todo_scan":               "find TODO, FIXME and HACK markers",
	"todos":                   "track a task list",
	"tool_pipeline":           "chain several tool calls in one step",
	"type_hierarchy":          "Go: which types satisfy which interfaces",
	"usage":                   "check token and cost usage",
	"web_fetch":               "read a URL as text",
	"web_search":              "search the web",
}

// maxHintRunes bounds a hint taken from a tool's own description.
const maxHintRunes = 60

// ToolHint is the short "use it when" text for the deferred tool index.
func ToolHint(name, description string) string {
	if hint, ok := toolHints[name]; ok {
		return hint
	}
	summary := strings.TrimSuffix(toolSummary(description), "…")
	if end := strings.IndexAny(summary, ".;:("); end > 0 {
		summary = summary[:end]
	}
	summary = strings.TrimSpace(summary)
	if utf8.RuneCountInString(summary) <= maxHintRunes {
		return summary
	}
	runes := []rune(summary)[:maxHintRunes]
	cut := string(runes)
	if space := strings.LastIndexByte(cut, ' '); space > maxHintRunes/2 {
		cut = cut[:space]
	}
	return strings.TrimSpace(cut) + "…"
}

// toolAliases are the words people use for a tool's job when they do not
// know its name, in English and Turkish, including the libraries a model
// would otherwise reach for through the shell (a request to "take a
// playwright screenshot" is a browser task). They are compared after accent
// folding and by prefix, like category terms, but rank above them because
// they point at one tool rather than a group.
var toolAliases = map[string][]string{
	"browser":          {"webpage", "page", "playwright", "puppeteer", "selenium", "headless", "screenshot", "click", "form", "login", "render", "sayfa", "goruntu", "ekran", "tikla", "giris", "tarayici", "site"},
	"computer":         {"desktop", "window", "gui", "mouse", "keyboard", "click", "screen", "native", "app", "masaustu", "pencere", "fare", "klavye", "tikla", "ekran", "uygulama"},
	"fetch":            {"url", "curl", "wget", "http", "page", "html", "read", "sayfa", "oku", "indir"},
	"web_fetch":        {"url", "curl", "wget", "http", "page", "html", "sayfa", "oku"},
	"web_search":       {"google", "search", "internet", "news", "latest", "ara", "arama", "guncel", "haber"},
	"agentic_fetch":    {"research", "investigate", "sources", "arastir", "arastirma", "kaynak"},
	"download":         {"curl", "wget", "save", "file", "kaydet", "indir"},
	"local_app":        {"server", "dev", "localhost", "serve", "start", "port", "sunucu", "baslat", "calistir"},
	"app_loop":         {"run", "dev", "server", "watch", "reload", "exercise", "flow", "calistir", "izle", "dene"},
	"tool_pipeline":    {"chain", "batch", "sequence", "steps", "automate", "zincir", "toplu", "sira", "otomatik"},
	"android":          {"phone", "mobile", "emulator", "adb", "apk", "telefon", "mobil"},
	"isolated_desktop": {"sandbox", "vm", "virtual", "safe", "disposable", "sanal", "guvenli", "izole"},
	"remote_node":      {"ssh", "remote", "machine", "server", "linux", "uzak", "makine", "sunucu"},
	"debugger":         {"breakpoint", "step", "delve", "dlv", "inspect", "variable", "durdur", "degisken"},
	"test_run":         {"tests", "failing", "suite", "unit", "calistir"},
	"ui_verify":        {"screenshot", "render", "layout", "responsive", "visual", "check", "gorunum", "duzen", "kontrol"},
	"a11y_audit":       {"accessibility", "contrast", "aria", "screenreader", "keyboard", "erisilebilirlik", "kontrast"},
	"visual_diff":      {"compare", "screenshot", "pixel", "regression", "karsilastir", "fark"},
	"video":            {"record", "recording", "animation", "motion", "kayit", "animasyon"},
	"memory":           {"remember", "note", "preference", "hatirla", "not", "tercih"},
	"todos":            {"checklist", "steps", "progress", "liste", "adim", "ilerleme"},
	"screen_mark":      {"marked", "highlight", "region", "selection", "isaret", "secim", "bolge"},
	"inspect_file":     {"zip", "tar", "archive", "sqlite", "database", "notebook", "ipynb", "arsiv", "veritabani"},
	"design_system":    {"tokens", "palette", "typography", "theme", "brand", "renk", "tema", "tipografi", "marka"},
	"design_search":    {"style", "font", "palette", "inspiration", "stil", "yazitipi", "ilham"},
	"scenario":         {"e2e", "end", "flow", "journey", "senaryo", "akis"},
	"atlas_config":     {"setting", "model", "provider", "role", "enable", "disable", "ayar", "rol", "kapat"},
	"git_diff":         {"changes", "changed", "diff", "degisiklik", "fark"},
	"git_log":          {"history", "commits", "gecmis"},
	"lsp_references":   {"usages", "uses", "callers", "kullanim"},
	"lsp_rename":       {"rename", "refactor", "yeniden", "adlandir"},
	"lsp_diagnostics":  {"errors", "compile", "typecheck", "warnings", "hata", "derleme", "uyari"},
	"project_map":      {"overview", "structure", "architecture", "entry", "yapi", "mimari", "genel"},
	"verify":           {"check", "prove", "confirm", "kontrol", "kanit", "dogrula"},
	"agent":            {"subagent", "parallel", "delegate", "help", "yardim", "paralel"}, //nolint:misspell // Turkish term.
	"delegate":         {"subagent", "parallel", "handoff", "devret", "paralel"},          //nolint:misspell // Turkish term.
	"job_output":       {"background", "output", "progress", "cikti"},
	"log_tail":         {"log", "logs", "tail", "kayit", "gunluk"},
	"scan_secrets":     {"secret", "key", "token", "password", "leak", "sifre", "anahtar", "sizinti"},
}

// aliasTokens returns a tool's folded alias words.
func aliasTokens(name string) []string {
	var out []string
	for _, alias := range toolAliases[name] {
		out = append(out, searchTokens(alias)...)
	}
	return out
}

// closestToolLimit is how many near matches a search with no real match
// suggests.
const closestToolLimit = 3

// closestTools suggests tools for a query that matched nothing, by spelling
// similarity to their names, aliases, hints and category terms. It catches
// typos and unexpected inflections ("tarayicidaki", "screnshot") so that a
// search never comes back empty-handed while a fitting tool exists. Only a
// clearly similar word counts; the suggestions are never loaded.
func closestTools(palette []fantasy.AgentTool, query []string) []fantasy.ToolInfo {
	type candidate struct {
		info  fantasy.ToolInfo
		score float64
	}
	var ranked []candidate
	for _, tool := range palette {
		info := tool.Info()
		if info.Name == ToolSearchToolName {
			continue
		}
		words := slices.Concat(searchTokens(info.Name), aliasTokens(info.Name), searchTokens(ToolHint(info.Name, info.Description)), toolCategories[ToolCategory(info.Name)].Terms)
		score := 0.0
		for _, token := range query {
			best := 0.0
			for _, word := range words {
				best = max(best, wordSimilarity(token, word))
			}
			if best >= 0.7 {
				score += best
			}
		}
		if score > 0 {
			ranked = append(ranked, candidate{info, score})
		}
	}
	slices.SortFunc(ranked, func(a, b candidate) int {
		if a.score != b.score {
			if a.score > b.score {
				return -1
			}
			return 1
		}
		return strings.Compare(a.info.Name, b.info.Name)
	})
	out := make([]fantasy.ToolInfo, 0, closestToolLimit)
	for _, c := range ranked[:min(len(ranked), closestToolLimit)] {
		out = append(out, c.info)
	}
	return out
}

// wordSimilarity is 1 minus the edit distance over the longer length,
// comparing a long token only up to the word's length plus two, so an
// inflected form ("goruntusune") still resembles its stem ("goruntu").
// Words shorter than four letters are too ambiguous to compare.
func wordSimilarity(token, word string) float64 {
	if len(token) < 4 || len(word) < 4 {
		return 0
	}
	if len(token) > len(word)+2 {
		token = token[:len(word)+2]
	}
	distance := editDistance(token, word)
	return 1 - float64(distance)/float64(max(len(token), len(word)))
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
