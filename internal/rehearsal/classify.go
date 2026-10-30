package rehearsal

import (
	"regexp"
	"strings"
)

// Risk explains why a command deserves a rehearsal first.
type Risk struct {
	Category string `json:"category"`
	Reason   string `json:"reason"`
}

type riskRule struct {
	category string
	reason   string
	pattern  *regexp.Regexp
}

// The rules are deliberately conservative: they name commands that are
// hard or impossible to undo, or that rewrite many files at once.
var riskRules = []riskRule{
	{"delete", "recursive or forced deletion", regexp.MustCompile(`(?i)(^|[\s;&|(])(rm\s+(-[a-z]*[rf][a-z]*\s+)+|rm\s+--recursive|rmdir\s+/s|del\s+(/[a-z]\s+)*/s|rd\s+/s|remove-item\b[^|;&]*-recurse)`)},
	{"delete", "find -delete removes every match", regexp.MustCompile(`(?i)\bfind\b[^|;&]*\s-delete\b`)},
	{"vcs", "discards uncommitted work or rewrites history", regexp.MustCompile(`(?i)\bgit\s+(reset\s+--hard|clean\s+(-[a-z]*f|--force)|checkout\s+(--\s+)?\.(\s|$)|restore\s+(--source\S*\s+)?\.(\s|$)|push\s+[^|;&]*(--force|-f)\b|filter-(branch|repo)|branch\s+-D\b)`)},
	{"database", "destructive SQL", regexp.MustCompile(`(?i)\b(drop\s+(table|database|schema|index)|truncate\s+(table\s+)?\w+|delete\s+from\s+\w+\s*(;|$|"|'))`)},
	{"database", "schema migration", regexp.MustCompile(`(?i)\b(prisma\s+migrate|alembic\s+(upgrade|downgrade)|rails\s+db:(migrate|rollback|drop|reset)|rake\s+db:(migrate|rollback|drop|reset)|flyway\s+(migrate|clean)|goose\s+(up|down|reset)|migrate\s+(-database|up|down|drop)|sequelize(-cli)?\s+db:migrate|knex\s+migrate|typeorm\s+migration:(run|revert)|manage\.py\s+(migrate|flush)|diesel\s+migration\s+(run|revert|redo))`)},
	{"dependencies", "changes dependency versions across the project", regexp.MustCompile(`(?i)\b(npm\s+(update|upgrade|audit\s+fix(\s+--force)?)|(pnpm|yarn)\s+(up|upgrade|upgrade-interactive)|npx\s+npm-check-updates|ncu\s+-u|pip3?\s+install\s+[^|;&]*(-U|--upgrade)|cargo\s+(update|upgrade)|go\s+get\s+[^|;&]*-u\b|go\s+get\s+all|composer\s+(update|require)|bundle\s+update|poetry\s+update|dotnet\s+(add\s+package|outdated\s+--upgrade))`)},
	{"mass-edit", "rewrites many files in place", regexp.MustCompile(`(?i)(\bfind\b[^|;&]*-exec\s+(sed|perl)\s+[^|;&]*-i|xargs\s+(sed|perl)\s+[^|;&]*-i|\b(sed|perl)\s+-[a-z]*i[a-z]*\b[^|;&]*(\*|-r)|\b(gofmt|goimports|gofumpt)\s+-w\s+\.|prettier\s+[^|;&]*--write\s+\.|eslint\s+[^|;&]*--fix\s+\.|jscodeshift|codemod|go\s+fix\s+\./\.\.\.|gofmt\s+-r\b|git\s+mv\s+[^|;&]*\*)`)},
}

// Classify reports why a shell command is risky enough to rehearse first.
func Classify(command string) (Risk, bool) {
	command = strings.TrimSpace(command)
	if command == "" {
		return Risk{}, false
	}
	for _, rule := range riskRules {
		if rule.pattern.MatchString(command) {
			return Risk{Category: rule.category, Reason: rule.reason}, true
		}
	}
	return Risk{}, false
}

// Hint is the one-line suggestion shown beside a risky command.
func (r Risk) Hint() string {
	return "Risky command (" + r.Reason + "). Consider the rehearse tool to preview it in an isolated copy first."
}
