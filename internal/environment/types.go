package environment

import (
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

type ToolRequirement struct {
	Name       string `json:"name"`
	Constraint string `json:"constraint"`
	Manifest   string `json:"manifest"`
}

type Command struct {
	Env          []string `json:"env,omitempty"`
	Argv         []string `json:"argv"`
	Directory    string   `json:"directory"`
	NeedsNetwork bool     `json:"needs_network"`
}

type EnvironmentPlan struct {
	Root              string                        `json:"root"`
	SourceFingerprint string                        `json:"source_fingerprint"`
	ExecutionOS       string                        `json:"execution_os"`
	Requirements      []ToolRequirement             `json:"requirements"`
	Sources           []engineering.SourceReference `json:"sources"`
	Commands          []Command                     `json:"commands"`
	Conflicts         []string                      `json:"conflicts"`
}

type EnvironmentReport struct {
	PlanHash         string            `json:"plan_hash"`
	ObservedVersions map[string]string `json:"observed_versions"`
	Passed           bool              `json:"passed"`
	Gaps             []string          `json:"gaps"`
}
