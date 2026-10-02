package environment

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

func ValidatePlan(ctx context.Context, plan EnvironmentPlan) error {
	current, err := Inspect(ctx, plan.Root)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, plan) {
		return fmt.Errorf("environment plan changed or was modified; inspect again")
	}
	for _, name := range []string{".venv", ".atlas-env", "node_modules"} {
		dir := filepath.Join(plan.Root, name)
		info, err := os.Lstat(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || filepath.Clean(resolved) != filepath.Clean(dir) {
			return fmt.Errorf("preparation directory %s must be a project-local directory without symlinks", name)
		}
	}
	return nil
}

func PlanHash(plan EnvironmentPlan) (string, error) {
	data, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	return engineering.Hash(string(data)), nil
}
