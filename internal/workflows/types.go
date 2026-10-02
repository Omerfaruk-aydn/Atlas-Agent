// Package workflows validates declarative recipes without executing their data.
package workflows

import (
	"encoding/json"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
)

type Parameter struct {
	Name     string          `json:"name"`
	Type     string          `json:"type"`
	Required bool            `json:"required"`
	Default  json.RawMessage `json:"default,omitempty"`
}

type Check struct {
	Name      string   `json:"name"`
	Directory string   `json:"directory"`
	Argv      []string `json:"argv"`
}

type Step struct {
	ID         string   `json:"id"`
	Role       string   `json:"role"`
	Prompt     string   `json:"prompt"`
	DependsOn  []string `json:"depends_on,omitempty"`
	OwnedPaths []string `json:"owned_paths"`
	Criteria   []string `json:"criteria"`
	Checks     []Check  `json:"checks,omitempty"`
}

type Requirement struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	StepIDs     []string `json:"step_ids"`
}

type Recipe struct {
	ID           string        `json:"id"`
	Version      int           `json:"version"`
	Parameters   []Parameter   `json:"parameters,omitempty"`
	Steps        []Step        `json:"steps"`
	Requirements []Requirement `json:"requirements"`
}

type Compiled struct {
	RecipeID       string                   `json:"recipe_id"`
	RecipeHash     string                   `json:"recipe_hash"`
	ParametersHash string                   `json:"parameters_hash"`
	Tasks          []session.Todo           `json:"tasks"`
	Plan           engineering.DeliveryPlan `json:"plan"`
	Checks         map[string][]Check       `json:"checks"`
	StepTaskIDs    map[string]string        `json:"step_task_ids"`
}

type RecipeRun struct {
	ID             string `json:"id"`
	SessionID      string `json:"session_id"`
	RecipeHash     string `json:"recipe_hash"`
	ParametersHash string `json:"parameters_hash"`
	Status         string `json:"status"`
}
