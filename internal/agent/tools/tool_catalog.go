package tools

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// CoreToolNames are the tools whose schemas are sent from the first step
// when deferred tool schemas are enabled. They cover reading, searching,
// editing, running commands, asking the user and discovering the rest, which
// is what a greeting or an ordinary coding request needs before it knows
// anything more specific. Everything else stays authorized and is loaded
// through tool_search.
var CoreToolNames = []string{
	ToolSearchToolName, "bash", "view", "edit", "multiedit", "write",
	"grep", "glob", "ls", "question",
}

// toolCategory groups tools for the capability index and tool_search. Terms
// cover common English and Turkish task phrasing; they are matched after
// accent folding and by prefix, so inflected forms such as "tarayıcıda" or
// "screenshots" still match their stem.
type toolCategory struct {
	ID    string
	Terms []string
}

var toolCategories = map[string]toolCategory{
	"files":    {"files", []string{"file", "files", "read", "write", "edit", "create", "directory", "folder", "path", "content", "dosya", "oku", "yaz", "duzenle", "olustur", "klasor", "dizin", "icerik"}},
	"search":   {"search", []string{"search", "find", "grep", "glob", "pattern", "regex", "locate", "ara", "bul", "arama"}},
	"shell":    {"shell", []string{"shell", "terminal", "command", "run", "execute", "script", "process", "background", "job", "cli", "program", "executable", "profile", "ffmpeg", "komut", "calistir", "betik", "surec", "profil"}},
	"web":      {"web", []string{"web", "url", "http", "https", "internet", "website", "page", "fetch", "download", "research", "documentation", "docs", "online", "site", "sayfa", "indir", "arastir", "arastirma", "dokuman"}},
	"browser":  {"browser", []string{"browser", "chrome", "chromium", "navigate", "dom", "tab", "click", "form", "login", "webpage", "screenshot", "tarayici", "sekme", "tikla", "gezin", "ekran"}},
	"desktop":  {"desktop", []string{"desktop", "computer", "gui", "window", "application", "app", "mouse", "keyboard", "screen", "screenshot", "notepad", "explorer", "calculator", "masaustu", "bilgisayar", "pencere", "uygulama", "fare", "klavye", "ekran", "hesap"}},
	"pipeline": {"automation", []string{"pipeline", "automation", "workflow", "multistep", "chain", "batch", "sequence", "flow", "recipe", "otomasyon", "zincir", "akis", "adim"}},
	"agents":   {"agents", []string{"agent", "subagent", "delegate", "parallel", "specialist", "review", "team", "orchestrate", "debate", "ajan", "alt", "devret", "paralel", "uzman", "inceleme", "ekip"}}, //nolint:misspell // Turkish term.
	"planning": {"planning", []string{"todo", "todos", "plan", "task", "tasks", "checklist", "goal", "requirement", "ledger", "gorev", "liste", "hedef", "gereksinim", "yapilacak"}},
	"memory":   {"memory", []string{"memory", "remember", "fact", "facts", "history", "session", "recall", "hafiza", "bellek", "hatirla", "gecmis", "oturum"}},
	"code":     {"code intelligence", []string{"symbol", "definition", "reference", "references", "rename", "refactor", "lsp", "diagnostics", "callers", "hierarchy", "type", "import", "graph", "map", "semantic", "tanim", "referans", "sembol", "yeniden", "kod", "teshis"}},
	"testing":  {"testing", []string{"test", "tests", "verify", "verification", "coverage", "lint", "benchmark", "regression", "reproduce", "mutation", "bug", "debug", "dogrula", "dogrulama", "kapsam", "hata", "yeniden", "uret"}},
	"ui":       {"ui", []string{"ui", "ux", "accessibility", "a11y", "visual", "design", "layout", "interaction", "video", "screenshot", "erisilebilirlik", "gorsel", "tasarim", "arayuz"}},
	"git":      {"git", []string{"git", "commit", "branch", "diff", "blame", "log", "pull", "request", "pr", "merge", "conflict", "changelog", "worktree", "dal", "birlestir", "cakisma"}},
	"config":   {"atlas settings", []string{"config", "configuration", "settings", "setting", "model", "provider", "usage", "cost", "logs", "skill", "skills", "ayar", "ayarlar", "yapilandirma", "kullanim", "maliyet", "yetenek"}},
	"workflow": {"workflow control", []string{"workflow", "recover", "resume", "status", "checkpoint", "pause", "budget", "scenario", "kurtar", "devam", "durum", "duraklat", "butce"}},
	"quality":  {"quality and security", []string{"security", "secret", "secrets", "vulnerability", "audit", "dependency", "dependencies", "docker", "kubernetes", "terraform", "ci", "metrics", "guvenlik", "gizli", "zafiyet", "denetim", "bagimlilik"}},
	"docs":     {"documentation", []string{"docs", "documentation", "markdown", "readme", "docstring", "belge", "dokumantasyon"}},
	"platform": {"platform", []string{"jobs", "board", "vault", "heartbeat", "cron", "schedule", "source", "kasa", "zamanla", "pano"}},
	"devices":  {"devices", []string{"android", "emulator", "adb", "apk", "sandbox", "isolated", "isolation", "izole", "yalitilmis", "node", "nodes", "remote", "uzak", "cihaz", "telefon", "phone", "mobile", "mobil", "wsl", "vm", "sanal", "mark", "isaret", "isaretle", "devloop", "canli", "live"}},
	"mcp":      {"mcp", []string{"mcp", "server", "resource", "resources", "integration", "integrations", "connector", "provider", "account", "login", "installed", "dependency", "entegrasyon", "kaynak", "saglayici", "hesap", "bagimlilik", "kurulu"}},
	"apps":     {"local apps", []string{"app", "apps", "application", "build", "create", "tracker", "inventory", "stock", "crm", "database", "backup", "restore", "localhost", "uygulama", "olustur", "takip", "stok", "envanter", "yedek", "geri", "yukle"}},
}

var toolCategoryByName = map[string]string{
	"view": "files", "edit": "files", "multiedit": "files", "write": "files", "ls": "files", "inspect_file": "files",
	"grep": "search", "glob": "search", "sourcegraph": "search", "semantic_code_search": "search",
	"bash": "shell", "job_output": "shell", "job_kill": "shell", "log_tail": "shell",
	"fetch": "web", "agentic_fetch": "web", "download": "web", "web_fetch": "web", "web_search": "web",
	"browser":        "browser",
	"captcha_detect": "browser", "captcha_solve": "browser",
	"computer": "desktop", "tool_pipeline": "pipeline",
	"agent": "agents", "delegate": "agents", "orchestrate": "agents", "debate": "agents", "vibe": "agents",
	"todos": "planning", "goal": "planning", "requirement_trace": "planning", "exit_plan_mode": "planning", "question": "planning",
	"memory": "memory", "facts": "memory", "session_search": "memory", "context_select": "memory",
	"code_query": "code", "code_map": "code", "project_map": "code", "dead_code": "code", "type_hierarchy": "code", "import_graph": "code",
	"impact_analysis": "code", "code_metrics": "code", "api_surface": "code", "anti_pattern_scan": "code", "todo_scan": "code",
	"verify": "testing", "test_select": "testing", "mutation_test": "testing", "test_run": "testing", "coverage_report": "testing",
	"lint_run": "testing", "bug_reproduce": "testing", "repro_minimize": "testing", "benchmark_compare": "testing",
	"failure_history": "testing", "generate_tests": "testing", "fixture_test": "testing", "debugger": "testing",
	"ui_verify": "ui", "a11y_audit": "ui", "interaction_audit": "ui", "visual_diff": "ui", "design_search": "ui", "design_system": "ui", "video": "ui",
	"worktree": "git", "changelog_gen": "git", "pr_describe": "git", "pre_commit_guard": "git", "audit_trail": "git", "github_pr_view": "git",
	"atlas_config": "config", "atlas_info": "config", "atlas_logs": "config", "usage": "config", "skill_manage": "config",
	"workflow": "workflow", "scenario": "workflow",
	"scan_secrets": "quality", "security_scan": "quality", "dep_audit": "quality", "docker_build_explain": "quality",
	"k8s_manifest_lint": "quality", "terraform_lint": "quality", "ci_cd_pipeline_debugger": "quality",
	"cloud_resource_costs": "quality", "env_var_audit": "quality", "metric_export": "quality",
	"api_probe": "testing", "contract_diff": "code", "migration_rehearse": "testing",
	"stress_journey": "testing", "undo_feature": "git", "handover": "docs",
	"time_lab": "testing", "dataset_forge": "testing", "assumption_check": "testing", "data_boundary": "quality",
	"cli_bridge": "shell",
	"doc_index":  "docs", "generate_docstring": "docs",
	"agent_jobs": "platform", "task_board": "platform", "source_memory": "memory",
	"list_mcp_resources": "mcp", "read_mcp_resource": "mcp",
	"isolated_desktop": "devices", "android": "devices", "remote_node": "devices", "remote_support": "devices", "screen_mark": "devices", "app_loop": "devices",
	"integrations": "mcp", "local_app": "apps",
}

// ToolCategory reports the capability group of a tool by name.
func ToolCategory(name string) string {
	if category, ok := toolCategoryByName[name]; ok {
		return category
	}
	for prefix, category := range map[string]string{"lsp_": "code", "git_": "git", "mcp_": "mcp", "team_": "agents", "job_": "shell"} {
		if strings.HasPrefix(name, prefix) {
			return category
		}
	}
	return "other"
}

// toolSummary is the first sentence of a description, bounded for search
// results so that a result list never carries complete tool documentation.
func toolSummary(description string) string {
	text := ""
	for line := range strings.SplitSeq(description, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#*- "))
		if line == "" || strings.HasPrefix(line, "<") {
			continue
		}
		text = line
		break
	}
	if end := strings.Index(text, ". "); end > 0 {
		text = text[:end+1]
	}
	const limit = 180
	if utf8.RuneCountInString(text) > limit {
		runes := []rune(text)
		text = strings.TrimSpace(string(runes[:limit])) + "…"
	}
	return text
}

// foldText lowercases and folds Turkish and common Latin accents to ASCII so
// "Tarayıcı", "TARAYICI" and "tarayici" compare equal.
func foldText(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		switch r {
		case 'İ', 'I', 'ı':
			r = 'i'
		case 'Ş', 'ş':
			r = 's'
		case 'Ğ', 'ğ':
			r = 'g'
		case 'Ü', 'ü':
			r = 'u'
		case 'Ö', 'ö':
			r = 'o'
		case 'Ç', 'ç':
			r = 'c'
		case 'Â', 'â', 'Á', 'á', 'À', 'à':
			r = 'a'
		case 'Î', 'î':
			r = 'i'
		case 'Û', 'û':
			r = 'u'
		case '̇':
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// searchTokens splits text into folded alphanumeric words. Underscores,
// dots and dashes separate words so tool names split into their parts.
func searchTokens(text string) []string {
	return strings.FieldsFunc(foldText(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

var searchStopwords = []string{
	"a", "an", "the", "to", "and", "or", "of", "for", "in", "on", "with", "by", "it", "is", "be", "this", "that", "please",
	"use", "using", "tool", "tools", "need", "want", "can", "do", "make", "some", "my", "me",
	"bir", "ve", "ile", "icin", "bu", "su", "o", "de", "da", "mi", "mu", "ne", "nasil", "lutfen", "arac", "araci", "araclar", "kullan", "yap", "et", "olan", "gibi", "bana", "beni",
}

// termMatches reports whether a query token and an index term name the same
// word. Prefix matching above four letters absorbs inflection ("tarayicida",
// "screenshots") without matching unrelated short words.
func termMatches(token, term string) bool {
	if token == term {
		return true
	}
	if len(term) >= 4 && strings.HasPrefix(token, term) {
		return true
	}
	return len(token) >= 4 && strings.HasPrefix(term, token)
}

// toolSearchScore ranks one tool for a query deterministically. Exact names
// dominate, then name parts, then curated category terms, then words from
// the tool's own description.
func toolSearchScore(info fantasy.ToolInfo, query []string) int {
	category := ToolCategory(info.Name)
	nameParts := searchTokens(info.Name)
	aliases := aliasTokens(info.Name)
	terms := toolCategories[category].Terms
	hintWords := searchTokens(ToolHint(info.Name, info.Description))
	descWords := searchTokens(toolSummary(info.Description) + " " + firstRunes(info.Description, 600))
	score := 0
	matched := 0
	for _, token := range query {
		best := 0
		switch {
		case token == foldText(info.Name):
			best = 100
		case slices.ContainsFunc(nameParts, func(part string) bool { return termMatches(token, part) }):
			best = 40
		case slices.ContainsFunc(aliases, func(alias string) bool { return termMatches(token, alias) }):
			best = 30
		case token == category || slices.ContainsFunc(terms, func(term string) bool { return termMatches(token, term) }):
			best = 25
		case slices.ContainsFunc(hintWords, func(word string) bool { return len(word) > 2 && termMatches(token, word) }):
			best = 12
		case slices.ContainsFunc(descWords, func(word string) bool { return termMatches(token, word) }):
			best = 6
		}
		if best > 0 {
			matched++
		}
		score += best
	}
	// Prefer tools that explain every word of a multi-word request.
	if matched == len(query) && len(query) > 1 {
		score += 10
	}
	return score
}

func firstRunes(text string, n int) string {
	if utf8.RuneCountInString(text) <= n {
		return text
	}
	return string([]rune(text)[:n])
}

// toolIntents preloads small tool groups when a request clearly names a
// capability. This is only an optimization: a missed or wrong guess still
// leaves tool_search, and nothing outside the live authorized palette is
// ever loaded.
var toolIntents = []struct {
	terms []string
	tools []string
}{
	{[]string{"browser", "chrome", "chromium", "tarayici", "webpage"}, []string{"browser", "tool_pipeline"}},
	{[]string{"desktop", "masaustu", "computer", "bilgisayar", "notepad", "calculator", "pencere"}, []string{"computer", "tool_pipeline"}},
	{[]string{"test"}, []string{"test_run"}},
	{[]string{"lint"}, []string{"lint_run"}},
	{[]string{"coverage", "kapsam"}, []string{"coverage_report"}},
	{[]string{"benchmark"}, []string{"benchmark_compare"}},
	{[]string{"git"}, []string{"git_status", "git_diff"}},
	{[]string{"memory", "bellek", "hafiza"}, []string{"memory"}},
	{[]string{"pipeline", "otomasyon"}, []string{"tool_pipeline"}},
}

// IntentTools returns tool names suggested by explicit capability words in a
// user request.
func IntentTools(text string) []string {
	words := searchTokens(text)
	folded := foldText(text)
	var out []string
	for _, intent := range toolIntents {
		if slices.ContainsFunc(words, func(word string) bool {
			return slices.ContainsFunc(intent.terms, func(term string) bool { return termMatches(word, term) })
		}) {
			for _, name := range intent.tools {
				if !slices.Contains(out, name) {
					out = append(out, name)
				}
			}
		}
	}
	add := func(names ...string) {
		for _, name := range names {
			if !slices.Contains(out, name) {
				out = append(out, name)
			}
		}
	}
	// Implicit website tasks need an action, not just a mentioned service.
	// Code and API discussions must not preload an interactive browser.
	codeDiscussion := slices.ContainsFunc(words, func(word string) bool {
		return slices.Contains([]string{"api", "sdk", "client", "code", "kod", "explain", "acikla"}, word)
	})
	openAction := slices.ContainsFunc(words, func(word string) bool {
		return slices.Contains([]string{"open", "play", "navigate", "visit", "ac", "oynat"}, word) ||
			strings.HasPrefix(word, "acar") || strings.HasPrefix(word, "acabilir")
	})
	website := strings.Contains(folded, "https://") || strings.Contains(folded, "http://") ||
		slices.ContainsFunc(words, func(word string) bool { return termMatches(word, "youtube") })
	if website && openAction && !codeDiscussion {
		add("browser", "tool_pipeline")
	}
	// A named desktop application is GUI work whatever the application is:
	// Turkish possessive forms ("X uygulamasında") and common English
	// phrasing name an existing app rather than software to build.
	desktopApp := slices.ContainsFunc(words, func(word string) bool {
		return strings.HasPrefix(word, "uygulamasi") || strings.HasPrefix(word, "uygulamada") || strings.HasPrefix(word, "uygulamay")
	})
	for _, phrase := range []string{"desktop app", "masaustu uygulama", "app window", "application window", "in the app", "dosya gezgini", "file explorer", "save dialog", "open dialog", "kaydet penceres", "farkli kaydet"} {
		desktopApp = desktopApp || strings.Contains(folded, phrase)
	}
	// Built-in Windows components are often named without the word
	// application; this list predates the generic rule above.
	for _, app := range []string{"hesap makinesi", "not defteri", "dosya gezgini", "apple music"} {
		desktopApp = desktopApp || strings.Contains(folded, app)
	}
	if desktopApp && !codeDiscussion {
		add("computer", "tool_pipeline")
	}
	if strings.Contains(folded, "projenin harita") || strings.Contains(folded, "project map") || strings.Contains(folded, "code map") || strings.Contains(folded, "kod harita") {
		add("code_map", "project_map")
	}
	if strings.Contains(folded, "oturum gecmis") || strings.Contains(folded, "session history") {
		add("session_search")
	}
	if strings.Contains(folded, "guvenlik tara") || strings.Contains(folded, "security scan") {
		add("security_scan")
	}
	// Device intents need the device or environment named; a word like
	// "markdown" or "node.js" must not load device schemas.
	if slices.ContainsFunc(words, func(w string) bool {
		return slices.Contains([]string{"android", "adb", "apk", "emulator", "emulatoru", "emulatorde"}, w)
	}) && !codeDiscussion {
		add("android")
	}
	for _, phrase := range []string{"windows sandbox", "sandbox", "yalitilmis", "izole ortam", "ayri masaustu", "separate desktop", "isolated desktop", "arka planda uygulama"} {
		if strings.Contains(folded, phrase) && !codeDiscussion {
			add("isolated_desktop")
		}
	}
	for _, phrase := range []string{"remote node", "atlas node", "diger bilgisayar", "other computer", "birden fazla bilgisayar", "wsl", "linux makine", "uzak makine", "remote machine"} {
		if strings.Contains(folded, phrase) {
			add("remote_node")
		}
	}
	// Remote IT support needs the connected computer named; "support" alone
	// is too common.
	for _, phrase := range []string{"remote support", "uzak destek", "uzaktan destek", "karsi bilgisayar", "karsidaki bilgisayar", "bagli bilgisayar", "baglandigim bilgisayar", "connected computer", "supported computer", "destek oturumu", "support session", "it support", "bt destek"} {
		if strings.Contains(folded, phrase) {
			add("remote_support")
		}
	}
	for _, phrase := range []string{"<screen_mark", "isaretledigim", "isaretledim", "ekranda isaret", "marked region", "i marked", "screen mark"} {
		if strings.Contains(folded, phrase) {
			add("screen_mark")
		}
	}
	for _, phrase := range []string{"uygulamayi calistir", "gercek kullanici akisi", "gercek akis", "run the app", "real user flow", "dev server", "canli dogrula"} {
		if strings.Contains(folded, phrase) {
			add("app_loop", "scenario", "browser")
		}
	}
	if strings.Contains(folded, "atlas ayar") || strings.Contains(folded, "atlas settings") {
		add("atlas_config")
	}
	for _, phrase := range []string{"uygulamasi olustur", "uygulama olustur", "uygulamasi yap", "uygulama yap", "yerel uygulama", "create an app", "build an app", "build me an app", "local app", "atlas-app.json"} {
		if strings.Contains(folded, phrase) {
			add("local_app")
		}
	}
	for _, phrase := range []string{"entegrasyon", "integrations", "bagli hesap", "connected account", "mcp durum", "mcp status", "provider status", "saglayici durum"} {
		if strings.Contains(folded, phrase) {
			add("integrations")
		}
	}
	return out
}
