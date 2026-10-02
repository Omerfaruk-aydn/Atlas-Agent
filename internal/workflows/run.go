package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/google/uuid"
)

type savedRun struct {
	Run      RecipeRun `json:"run"`
	Compiled Compiled  `json:"compiled"`
}

func runNamespace(sessionID, id string) (string, error) {
	if _, err := uuid.Parse(id); err != nil || sessionID == "" {
		return "", fmt.Errorf("invalid recipe run identity")
	}
	return "recipe-runs-" + engineering.Hash(sessionID) + "-" + id[:2], nil
}

func ReadCompiled(ctx context.Context, store *engineering.Store, binding engineering.RecipeBinding) (Compiled, error) {
	_, err := runNamespace(binding.SessionID, binding.ID)
	if err != nil {
		return Compiled{}, err
	}
	data, err := store.ReadArtifact(ctx, binding.Record.Ref)
	if err != nil {
		return Compiled{}, err
	}
	var saved savedRun
	if err := json.Unmarshal(data, &saved); err != nil {
		return Compiled{}, err
	}
	c := saved.Compiled
	if saved.Run.ID != binding.ID || saved.Run.SessionID != binding.SessionID || c.RecipeID != binding.RecipeID || c.RecipeHash != binding.RecipeHash || c.ParametersHash != binding.ParametersHash || saved.Run.RecipeHash != c.RecipeHash || saved.Run.ParametersHash != c.ParametersHash || c.Plan.Root != binding.Root {
		return Compiled{}, fmt.Errorf("recipe artifact identity mismatch")
	}
	if err := session.ValidateTaskGraph(c.Tasks); err != nil {
		return Compiled{}, err
	}
	return c, c.Plan.Validate()
}

func ValidateBinding(ctx context.Context, store *engineering.Store, sessionID, recipeHash, parametersHash string) error {
	state, err := store.Read(ctx, sessionID)
	if err != nil || state.Recipe == nil {
		return err
	}
	if state.Recipe.Status != "active" || state.Recipe.RecipeHash != recipeHash || state.Recipe.ParametersHash != parametersHash {
		return fmt.Errorf("recipe changed or admission is incomplete; inspect the persisted run before continuing")
	}
	_, err = ReadCompiled(ctx, store, *state.Recipe)
	return err
}

func sameSpecifications(a, b []session.Todo) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || session.TaskFingerprint(a[i]) != session.TaskFingerprint(b[i]) {
			return false
		}
	}
	return true
}

// Install journals admission before graph mutation, then reconciles the graph
// and delivery state. It never executes a check, dispatches an agent or replaces
// existing work. A retry uses the same persisted compilation and identity.
func Install(ctx context.Context, store *engineering.Store, sessions session.Service, sessionID, root string, compiled Compiled) (RecipeRun, error) {
	if !filepath.IsAbs(root) {
		return RecipeRun{}, fmt.Errorf("recipe requires an absolute project root")
	}
	compiled.Plan.Root = filepath.Clean(root)
	if err := compiled.Plan.Validate(); err != nil {
		return RecipeRun{}, err
	}
	if err := session.ValidateTaskGraph(compiled.Tasks); err != nil {
		return RecipeRun{}, err
	}
	sess, err := sessions.Get(ctx, sessionID)
	if err != nil {
		return RecipeRun{}, err
	}
	state, err := store.Read(ctx, sessionID)
	if err != nil {
		return RecipeRun{}, err
	}
	var binding engineering.RecipeBinding
	if state.Recipe != nil {
		binding = *state.Recipe
		if binding.Root != compiled.Plan.Root || binding.RecipeID != compiled.RecipeID || binding.RecipeHash != compiled.RecipeHash || binding.ParametersHash != compiled.ParametersHash {
			return RecipeRun{}, fmt.Errorf("an existing recipe run cannot be overwritten")
		}
		saved, err := ReadCompiled(ctx, store, binding)
		if err != nil || !reflect.DeepEqual(saved, compiled) {
			return RecipeRun{}, fmt.Errorf("recipe compilation changed; inspect the saved run")
		}
		if binding.Status == "active" {
			if !sameSpecifications(sess.Todos, compiled.Tasks) || state.Delivery == nil {
				return RecipeRun{}, fmt.Errorf("active recipe graph changed; inspect before reconciliation")
			}
			return RecipeRun{ID: binding.ID, SessionID: sessionID, RecipeHash: binding.RecipeHash, ParametersHash: binding.ParametersHash, Status: binding.Status}, nil
		}
		if binding.Status != "prepared" || state.Delivery != nil {
			return RecipeRun{}, fmt.Errorf("recipe admission has conflicting state")
		}
	} else {
		if state.Delivery != nil || len(sess.Todos) != 0 {
			return RecipeRun{}, fmt.Errorf("recipe requires an empty session without a registered plan")
		}
		run := RecipeRun{ID: uuid.NewString(), SessionID: sessionID, RecipeHash: compiled.RecipeHash, ParametersHash: compiled.ParametersHash, Status: "prepared"}
		ns, err := runNamespace(sessionID, run.ID)
		if err != nil {
			return RecipeRun{}, err
		}
		data, err := json.Marshal(savedRun{Run: run, Compiled: compiled})
		if err != nil {
			return RecipeRun{}, err
		}
		record, err := store.PutRecordStrict(ctx, ns, run.ID, 0, data)
		if err != nil {
			return RecipeRun{}, err
		}
		binding = engineering.RecipeBinding{ID: run.ID, SessionID: sessionID, RecipeID: compiled.RecipeID, RecipeHash: compiled.RecipeHash, ParametersHash: compiled.ParametersHash, Root: compiled.Plan.Root, Status: "prepared", OriginalTodosFingerprint: session.TodosFingerprint(sess.Todos), Record: record}
		if err := store.UpdateRevision(ctx, sessionID, state.Revision, func(st *engineering.State) error {
			if st.Recipe != nil || st.Delivery != nil {
				return fmt.Errorf("concurrent recipe or delivery admission")
			}
			st.Recipe = &binding
			return nil
		}); err != nil {
			return RecipeRun{}, err
		}
	}
	if !sameSpecifications(sess.Todos, compiled.Tasks) {
		if session.TodosFingerprint(sess.Todos) != binding.OriginalTodosFingerprint {
			return RecipeRun{}, fmt.Errorf("session graph changed during recipe admission")
		}
		if _, err := sessions.CompareAndSwapTodos(ctx, sessionID, binding.OriginalTodosFingerprint, compiled.Tasks); err != nil {
			return RecipeRun{}, err
		}
	}
	if err := store.Update(ctx, sessionID, func(st *engineering.State) error {
		if st.Recipe == nil || st.Recipe.ID != binding.ID || st.Recipe.Status != "prepared" || st.Delivery != nil {
			return fmt.Errorf("recipe admission changed; reconcile the persisted run")
		}
		st.Delivery, st.Profile = &compiled.Plan, compiled.Plan.Profile
		st.Recipe.Status = "active"
		return nil
	}); err != nil {
		return RecipeRun{}, err
	}
	return RecipeRun{ID: binding.ID, SessionID: sessionID, RecipeHash: binding.RecipeHash, ParametersHash: binding.ParametersHash, Status: "active"}, nil
}
