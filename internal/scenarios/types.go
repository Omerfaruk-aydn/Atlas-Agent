// Package scenarios defines bounded, source-bound user interaction scenarios.
package scenarios

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/execution"
)

type Step struct {
	Action   string `json:"action"`
	Selector string `json:"selector,omitempty"`
	Value    string `json:"value,omitempty"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
}

type Assertion struct {
	Kind     string `json:"kind"`
	Selector string `json:"selector,omitempty"`
	Expected string `json:"expected,omitempty"`
}

type Scenario struct {
	ID         string      `json:"id"`
	Version    int         `json:"version"`
	Target     string      `json:"target"`
	URL        string      `json:"url,omitempty"`
	Argv       []string    `json:"argv,omitempty"`
	Steps      []Step      `json:"steps"`
	Assertions []Assertion `json:"assertions"`
	TimeoutMS  int64       `json:"timeout_ms,omitempty"`
	Width      int         `json:"width"`
	Height     int         `json:"height"`
	RunID      string      `json:"-"`
}

type ScenarioRun struct {
	ID                string                    `json:"id"`
	ScenarioID        string                    `json:"scenario_id"`
	SourceFingerprint string                    `json:"source_fingerprint"`
	Target            string                    `json:"target"`
	Status            string                    `json:"status"`
	Passed            bool                      `json:"passed"`
	Observed          bool                      `json:"observed"`
	Artifacts         []engineering.ArtifactRef `json:"artifacts"`
	Result            execution.Result          `json:"result"`
	Gaps              []string                  `json:"gaps"`
}

type Backend interface {
	Run(context.Context, Scenario) (ScenarioRun, error)
}

// EvidenceBackend is supplied by runtime adapters, never by scenario JSON.
type EvidenceBackend interface {
	Backend
	Source(context.Context) (string, error)
	ReadArtifact(context.Context, engineering.ArtifactRef) ([]byte, error)
}

func webURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && len(value) <= 8192 && parsed.Host != "" && parsed.User == nil && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

func dimensions(target string, width, height int) bool {
	if target == "web" {
		return width >= 240 && width <= 3840 && height >= 240 && height <= 2160
	}
	return width >= 20 && width <= 512 && height >= 5 && height <= 256
}

func Validate(ctx context.Context, scenario Scenario) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if scenario.Version != 1 || scenario.ID == "" || len(scenario.ID) > 128 || strings.ContainsAny(scenario.ID, "\x00\r\n") || scenario.Target != "web" && scenario.Target != "tui" || len(scenario.Steps) > 64 || len(scenario.Assertions) < 1 || len(scenario.Assertions) > 64 || scenario.TimeoutMS < 0 || scenario.TimeoutMS > 600000 || !dimensions(scenario.Target, scenario.Width, scenario.Height) {
		return fmt.Errorf("invalid scenario identity, version, limits or viewport")
	}
	if scenario.Target == "web" {
		if !webURL(scenario.URL) || len(scenario.Argv) != 0 {
			return fmt.Errorf("web scenario requires an HTTP(S) URL and no argv")
		}
	} else {
		if scenario.URL != "" || len(scenario.Argv) == 0 || len(scenario.Argv) > 128 || scenario.Argv[0] == "" {
			return fmt.Errorf("terminal scenario requires literal argv and no URL")
		}
		total := 0
		for _, arg := range scenario.Argv {
			total += len(arg)
			if len(arg) > 4096 || total > 16*1024 || strings.ContainsRune(arg, 0) {
				return fmt.Errorf("scenario argv exceeds bounds")
			}
		}
	}
	for _, step := range scenario.Steps {
		if len(step.Selector) > 512 || len(step.Value) > 4096 || strings.ContainsRune(step.Selector, 0) {
			return fmt.Errorf("scenario step exceeds bounds")
		}
		if step.Action == "resize" {
			if !dimensions(scenario.Target, step.Width, step.Height) || step.Value != "" || step.Selector != "" {
				return fmt.Errorf("invalid scenario resize")
			}
			continue
		}
		if step.Width != 0 || step.Height != 0 {
			return fmt.Errorf("only resize accepts dimensions")
		}
		if step.Action == "key" {
			switch step.Value {
			case "enter", "tab", "escape", "backspace", "delete", "arrowup", "arrowdown", "arrowleft", "arrowright":
			default:
				return fmt.Errorf("unsupported scenario key")
			}
		}
		if (step.Action == "click" || step.Action == "reload" || step.Action == "cancel") && step.Value != "" {
			return fmt.Errorf("scenario action does not accept a value")
		}
		if step.Action == "wait_text" && step.Value == "" {
			return fmt.Errorf("wait_text requires nonempty text")
		}
		if scenario.Target == "web" {
			switch step.Action {
			case "click", "type":
				if step.Selector == "" {
					return fmt.Errorf("web interaction requires a selector")
				}
			case "key", "reload":
			case "navigate":
				if !webURL(step.Value) {
					return fmt.Errorf("invalid step URL")
				}
			default:
				return fmt.Errorf("unsupported web scenario action")
			}
		} else {
			if step.Selector != "" {
				return fmt.Errorf("terminal actions have no DOM selector")
			}
			switch step.Action {
			case "input", "key", "wait_text", "cancel":
			default:
				return fmt.Errorf("unsupported terminal scenario action")
			}
		}
	}
	for _, assertion := range scenario.Assertions {
		if scenario.Target == "web" && (assertion.Kind == "visible" || assertion.Kind == "focused") && assertion.Expected != "" && assertion.Expected != "true" {
			return fmt.Errorf("visibility/focus assertions require positive observation")
		}
		if len(assertion.Selector) > 512 || len(assertion.Expected) > 4096 {
			return fmt.Errorf("scenario assertion exceeds bounds")
		}
		if scenario.Target == "web" {
			if assertion.Selector == "" || assertion.Kind != "text" && assertion.Kind != "value" && assertion.Kind != "visible" && assertion.Kind != "focused" {
				return fmt.Errorf("invalid web assertion")
			}
		} else if assertion.Selector != "" || assertion.Kind != "transcript" && assertion.Kind != "exit" && assertion.Kind != "status" {
			return fmt.Errorf("invalid terminal assertion")
		}
		if scenario.Target == "tui" {
			switch assertion.Kind {
			case "transcript":
				if assertion.Expected == "" {
					return fmt.Errorf("transcript assertion requires nonempty text")
				}
			case "exit":
				if _, err := strconv.Atoi(assertion.Expected); err != nil {
					return fmt.Errorf("exit assertion requires an integer")
				}
			case "status":
				switch assertion.Expected {
				case "succeeded", "failed", "cancelled":
				default:
					return fmt.Errorf("unsupported terminal status assertion")
				}
			}
		}
	}
	return nil
}

func Parse(ctx context.Context, data []byte) (Scenario, error) {
	if len(data) > 1024*1024 {
		return Scenario{}, fmt.Errorf("scenario JSON exceeds 1 MiB")
	}
	// Token validation rejects duplicate fields before typed decoding.
	decoder := json.NewDecoder(bytes.NewReader(data))
	var value func(int) error
	value = func(depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 32 {
			return fmt.Errorf("scenario JSON nesting exceeds bounds")
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, container := token.(json.Delim)
		if !container {
			return nil
		}
		keys := map[string]bool{}
		for decoder.More() {
			if delimiter == '{' {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || keys[name] || name != strings.ToLower(name) {
					return fmt.Errorf("duplicate or invalid scenario key")
				}
				keys[name] = true
			}
			if err := value(depth + 1); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	}
	if err := value(0); err != nil {
		return Scenario{}, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return Scenario{}, fmt.Errorf("multiple scenario JSON values")
	}
	var scenario Scenario
	decoder = json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&scenario); err != nil {
		return scenario, err
	}
	return scenario, Validate(ctx, scenario)
}
