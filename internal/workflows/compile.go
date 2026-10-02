package workflows

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
)

var (
	identifier  = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
	placeholder = regexp.MustCompile(`\$\{([a-zA-Z0-9_]+)\}`)
)

func relativePath(value string) bool {
	value = strings.ReplaceAll(value, `\`, "/")
	clean := path.Clean(value)
	return value != "" && len(value) <= 512 && !strings.ContainsAny(value, "*?:\x00\r\n") && !strings.HasPrefix(value, "/") && clean != ".." && !strings.HasPrefix(clean, "../")
}

func parameterValue(p Parameter, raw json.RawMessage) (any, string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || len(raw) > 16*1024 || !json.Valid(raw) {
		return nil, "", fmt.Errorf("parameter %s requires a bounded JSON value", p.Name)
	}
	if bytes.Equal(raw, []byte("null")) {
		return nil, "", fmt.Errorf("parameter %s cannot be null", p.Name)
	}
	switch p.Type {
	case "string":
		var value string
		if err := json.Unmarshal(raw, &value); err != nil || len(value) > 4096 || strings.ContainsRune(value, 0) {
			return nil, "", fmt.Errorf("parameter %s must be a string of at most 4096 bytes", p.Name)
		}
		return value, value, nil
	case "integer":
		if raw[0] == '"' {
			return nil, "", fmt.Errorf("parameter %s must be an integer, not a string", p.Name)
		}
		var value json.Number
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, "", fmt.Errorf("parameter %s must be an integer", p.Name)
		}
		number, err := value.Int64()
		if err != nil {
			return nil, "", fmt.Errorf("parameter %s must be an int64", p.Name)
		}
		return number, strconv.FormatInt(number, 10), nil
	case "boolean":
		var value bool
		if string(raw) == "null" {
			return nil, "", fmt.Errorf("parameter %s cannot be null", p.Name)
		}
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, "", fmt.Errorf("parameter %s must be a boolean", p.Name)
		}
		return value, strconv.FormatBool(value), nil
	default:
		return nil, "", fmt.Errorf("unsupported parameter type %s", p.Type)
	}
}

func expand(text string, values map[string]string) (string, error) {
	if strings.Contains(placeholder.ReplaceAllString(text, ""), "${") {
		return "", fmt.Errorf("malformed recipe parameter placeholder")
	}
	var missing string
	result := placeholder.ReplaceAllStringFunc(text, func(token string) string {
		name := placeholder.FindStringSubmatch(token)[1]
		value, ok := values[name]
		if !ok {
			missing = name
		}
		return value
	})
	if missing != "" {
		return "", fmt.Errorf("unknown or missing recipe parameter %s", missing)
	}
	return result, nil
}

func interpreted(check Check) bool {
	if len(check.Argv) == 0 {
		return false
	}
	program := strings.ToLower(strings.TrimSuffix(path.Base(strings.ReplaceAll(check.Argv[0], `\`, "/")), ".exe"))
	// Recipes declare executable checks rather than interpreter source. Reject
	// wrappers altogether so aliases and abbreviated flags cannot turn typed
	// parameters into code. Project executables still retain tool permissions.
	switch program {
	case "sh", "bash", "zsh", "dash", "fish", "cmd", "powershell", "pwsh", "node", "nodejs", "perl", "ruby", "busybox", "env", "exec", "command", "eval", "xargs":
		return true
	}
	return strings.HasPrefix(program, "python") || strings.HasPrefix(program, "pypy")
}

func Validate(ctx context.Context, recipe Recipe, roles []string) error {
	return validate(ctx, recipe, roles, true)
}

func validate(ctx context.Context, r Recipe, roles []string, enforceRoles bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !identifier.MatchString(r.ID) || r.Version != 1 || len(r.Parameters) > 32 || len(r.Steps) == 0 || len(r.Steps) > 64 || len(r.Requirements) == 0 || len(r.Requirements) > 128 {
		return fmt.Errorf("invalid recipe identity, version or collection bounds")
	}
	params := map[string]string{}
	for _, parameter := range r.Parameters {
		if !identifier.MatchString(parameter.Name) || strings.Contains(parameter.Name, "-") {
			return fmt.Errorf("invalid recipe parameter name")
		}
		if _, exists := params[parameter.Name]; exists {
			return fmt.Errorf("duplicate recipe parameter")
		}
		if parameter.Type != "string" && parameter.Type != "integer" && parameter.Type != "boolean" {
			return fmt.Errorf("unsupported recipe parameter type")
		}
		params[parameter.Name] = "placeholder"
		if len(parameter.Default) > 0 {
			if _, _, err := parameterValue(parameter, parameter.Default); err != nil {
				return err
			}
		}
	}
	steps := map[string]Step{}
	for _, step := range r.Steps {
		if !identifier.MatchString(step.ID) || !identifier.MatchString(step.Role) || enforceRoles && !slices.Contains(roles, step.Role) || strings.TrimSpace(step.Prompt) == "" || len(step.Prompt) > 4096 || len(step.DependsOn) > 32 || len(step.OwnedPaths) == 0 || len(step.OwnedPaths) > 32 || len(step.Criteria) == 0 || len(step.Criteria) > 16 || len(step.Checks) > 12 {
			return fmt.Errorf("invalid recipe step or unavailable role %s", step.ID)
		}
		if _, exists := steps[step.ID]; exists {
			return fmt.Errorf("duplicate recipe step")
		}
		steps[step.ID] = step
		if _, err := expand(step.Prompt, params); err != nil {
			return err
		}
		for _, scope := range step.OwnedPaths {
			value, err := expand(scope, params)
			if err != nil || !relativePath(value) {
				return fmt.Errorf("invalid recipe ownership")
			}
		}
		for _, criterion := range step.Criteria {
			if strings.TrimSpace(criterion) == "" || len(criterion) > 2048 {
				return fmt.Errorf("invalid recipe criterion")
			}
			if _, err := expand(criterion, params); err != nil {
				return err
			}
		}
		checkNames := map[string]bool{}
		for _, check := range step.Checks {
			if strings.TrimSpace(check.Name) == "" || len(check.Name) > 64 || checkNames[check.Name] || len(check.Argv) == 0 || len(check.Argv) > 128 || strings.Contains(check.Argv[0], "${") || check.Argv[0] == "" {
				return fmt.Errorf("invalid structured recipe check")
			}
			checkNames[check.Name] = true
			if check.Directory != "" {
				value, err := expand(check.Directory, params)
				if err != nil || !relativePath(value) {
					return fmt.Errorf("invalid recipe check directory")
				}
			}
			if interpreted(check) {
				return fmt.Errorf("recipe checks cannot embed interpreted command source")
			}
			total := 0
			for _, arg := range check.Argv {
				total += len(arg)
				if len(arg) > 4096 || strings.ContainsRune(arg, 0) {
					return fmt.Errorf("recipe argument exceeds limit")
				}
				if _, err := expand(arg, params); err != nil {
					return err
				}
			}
			if total > 16*1024 {
				return fmt.Errorf("recipe check arguments exceed 16 KiB")
			}
		}
	}
	seen, visiting := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return fmt.Errorf("recipe dependency cycle")
		}
		if seen[id] {
			return nil
		}
		step, exists := steps[id]
		if !exists {
			return fmt.Errorf("unknown recipe dependency %s", id)
		}
		visiting[id] = true
		deps := map[string]bool{}
		for _, dep := range step.DependsOn {
			if deps[dep] {
				return fmt.Errorf("duplicate recipe dependency")
			}
			deps[dep] = true
			if err := visit(dep); err != nil {
				return err
			}
		}
		visiting[id], seen[id] = false, true
		return nil
	}
	for _, step := range r.Steps {
		if err := visit(step.ID); err != nil {
			return err
		}
	}
	covered, requirements := map[string]bool{}, map[string]bool{}
	for _, req := range r.Requirements {
		if !identifier.MatchString(req.ID) || requirements[req.ID] || strings.TrimSpace(req.Description) == "" || len(req.Description) > 2048 || len(req.StepIDs) == 0 || len(req.StepIDs) > 32 {
			return fmt.Errorf("invalid recipe requirement")
		}
		requirements[req.ID] = true
		if _, err := expand(req.Description, params); err != nil {
			return err
		}
		links := map[string]bool{}
		for _, id := range req.StepIDs {
			if _, exists := steps[id]; !exists || links[id] {
				return fmt.Errorf("requirement references an unknown or duplicate step")
			}
			links[id], covered[id] = true, true
		}
	}
	if len(covered) != len(steps) {
		return fmt.Errorf("every recipe step requires requirement coverage")
	}
	return nil
}

func Compile(ctx context.Context, r Recipe, supplied map[string]json.RawMessage, roles []string) (Compiled, error) {
	if err := Validate(ctx, r, roles); err != nil {
		return Compiled{}, err
	}
	values, normalized := map[string]string{}, map[string]any{}
	declared := map[string]bool{}
	for _, parameter := range r.Parameters {
		declared[parameter.Name] = true
		raw, exists := supplied[parameter.Name]
		if !exists {
			raw = parameter.Default
		}
		if len(raw) == 0 {
			if parameter.Required {
				return Compiled{}, fmt.Errorf("missing required parameter %s", parameter.Name)
			}
			continue
		}
		value, text, err := parameterValue(parameter, raw)
		if err != nil {
			return Compiled{}, err
		}
		normalized[parameter.Name], values[parameter.Name] = value, text
	}
	for name := range supplied {
		if !declared[name] {
			return Compiled{}, fmt.Errorf("unknown parameter %s", name)
		}
	}
	data, _ := json.Marshal(r)
	parameters, _ := json.Marshal(normalized)
	result := Compiled{RecipeID: r.ID, RecipeHash: engineering.Hash(string(data)), ParametersHash: engineering.Hash(string(parameters)), Checks: map[string][]Check{}, StepTaskIDs: map[string]string{}, Plan: engineering.DeliveryPlan{Root: ".", Profile: "feature", TaskFingerprints: map[string]string{}}}
	if r.ID == "migration-review" {
		result.Plan.Profile = "migration"
	}
	steps := map[string]Step{}
	for _, step := range r.Steps {
		steps[step.ID] = step
		result.StepTaskIDs[step.ID] = "recipe-" + engineering.Hash(result.RecipeHash + "\x00" + step.ID)[:32]
	}
	var ordered []Step
	seen := map[string]bool{}
	var appendStep func(string)
	appendStep = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		for _, dep := range steps[id].DependsOn {
			appendStep(dep)
		}
		ordered = append(ordered, steps[id])
	}
	for _, step := range r.Steps {
		appendStep(step.ID)
	}
	for i, step := range ordered {
		if err := ctx.Err(); err != nil {
			return Compiled{}, err
		}
		prompt, err := expand(step.Prompt, values)
		if err != nil {
			return Compiled{}, err
		}
		task := session.Todo{ID: result.StepTaskIDs[step.ID], Agent: step.Role, Content: prompt, ActiveForm: "Executing " + r.ID + " / " + step.ID, Status: session.TodoStatusPending, Verification: "pending"}
		for _, dep := range step.DependsOn {
			task.DependsOn = append(task.DependsOn, result.StepTaskIDs[dep])
		}
		for _, scope := range step.OwnedPaths {
			value, err := expand(scope, values)
			if err != nil || !relativePath(value) {
				return Compiled{}, fmt.Errorf("expanded ownership escapes the project")
			}
			task.OwnedPaths = append(task.OwnedPaths, value)
		}
		for _, criterion := range step.Criteria {
			value, err := expand(criterion, values)
			if err != nil {
				return Compiled{}, err
			}
			task.AcceptanceCriteria = append(task.AcceptanceCriteria, value)
		}
		if err := session.ValidateTodo(task); err != nil {
			return Compiled{}, err
		}
		result.Tasks = append(result.Tasks, task)
		result.Plan.TaskFingerprints[task.ID] = session.TaskFingerprint(task)
		// Two consecutive topological steps per stage retain the 32-stage bound.
		// Dependencies inside a stage still control its ready wave.
		if i%2 == 0 {
			result.Plan.Stages = append(result.Plan.Stages, engineering.Stage{ID: fmt.Sprintf("phase-%d", i/2+1), Title: fmt.Sprintf("%s phase %d", r.ID, i/2+1)})
		}
		index := len(result.Plan.Stages) - 1
		result.Plan.Stages[index].TaskIDs = append(result.Plan.Stages[index].TaskIDs, task.ID)
		for _, check := range step.Checks {
			compiled := Check{Name: check.Name, Directory: ".", Argv: make([]string, len(check.Argv))}
			if check.Directory != "" {
				value, err := expand(check.Directory, values)
				if err != nil || !relativePath(value) {
					return Compiled{}, fmt.Errorf("expanded check directory escapes project")
				}
				compiled.Directory = value
			}
			total := 0
			for j, arg := range check.Argv {
				value, err := expand(arg, values)
				if err != nil {
					return Compiled{}, err
				}
				if strings.ContainsRune(value, 0) || len(value) > 4096 {
					return Compiled{}, fmt.Errorf("expanded argument exceeds bounds")
				}
				compiled.Argv[j] = value
				total += len(value)
			}
			if total > 16*1024 || interpreted(compiled) {
				return Compiled{}, fmt.Errorf("expanded recipe check is invalid")
			}
			result.Checks[task.ID] = append(result.Checks[task.ID], compiled)
		}
	}
	for _, requirement := range r.Requirements {
		description, err := expand(requirement.Description, values)
		if err != nil {
			return Compiled{}, err
		}
		compiled := engineering.Requirement{ID: requirement.ID, Description: description}
		for _, step := range requirement.StepIDs {
			compiled.TaskIDs = append(compiled.TaskIDs, result.StepTaskIDs[step])
		}
		result.Plan.Requirements = append(result.Plan.Requirements, compiled)
	}
	if err := session.ValidateTaskGraph(result.Tasks); err != nil {
		return Compiled{}, err
	}
	for _, stage := range result.Plan.Stages {
		checks := 0
		for _, id := range stage.TaskIDs {
			checks += len(result.Checks[id])
		}
		if checks > 12 {
			return Compiled{}, fmt.Errorf("recipe stage exceeds 12 machine checks")
		}
	}
	if err := result.Plan.Validate(); err != nil {
		return Compiled{}, err
	}
	return result, nil
}
