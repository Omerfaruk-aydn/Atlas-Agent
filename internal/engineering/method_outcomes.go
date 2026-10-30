package engineering

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
)

// A learned method is a hypothesis about what works until it has been seen
// working on tasks other than the one it was extracted from. Outcomes are
// recorded per task (session) and the method's standing is derived from them,
// so a method that was only ever observed once is never presented as an
// established practice.
const (
	// methodMeasuredTasks is how many distinct tasks must report an outcome
	// before a standing other than "candidate" is possible.
	methodMeasuredTasks = 3
	// methodMinSuccessRate is the share of those tasks that must have
	// succeeded for a method to count as effective.
	methodMinSuccessRate = 0.75

	StandingCandidate  = "candidate"
	StandingEffective  = "effective"
	StandingUnreliable = "unreliable"
)

// MethodOutcome is one task's result after using a method.
type MethodOutcome struct {
	TaskID  string `json:"task_id"`
	Success bool   `json:"success"`
	At      int64  `json:"at"`
}

// MethodStanding derives a method's standing from its recorded outcomes: the
// latest outcome of each distinct task counts once.
func MethodStanding(m LearnedMethod) (standing, reason string) {
	latest := map[string]bool{}
	for _, o := range m.Outcomes {
		latest[o.TaskID] = o.Success
	}
	tasks, wins := len(latest), 0
	for _, ok := range latest {
		if ok {
			wins++
		}
	}
	if tasks < methodMeasuredTasks {
		return StandingCandidate, fmt.Sprintf("measured on %d of %d required distinct tasks", tasks, methodMeasuredTasks)
	}
	rate := float64(wins) / float64(tasks)
	if rate < methodMinSuccessRate {
		return StandingUnreliable, fmt.Sprintf("succeeded on %d of %d tasks", wins, tasks)
	}
	return StandingEffective, fmt.Sprintf("succeeded on %d of %d tasks", wins, tasks)
}

// RecordMethodOutcome records how a task that used the method ended. A
// success is accepted only when the task's own work state shows its user
// requirements verified with current evidence; the model's word is not
// enough. Failures are always accepted.
func (s *Store) RecordMethodOutcome(ctx context.Context, root, methodID, taskSession string, success bool, taskFingerprint ...string) (LearnedMethod, error) {
	if taskSession == "" {
		return LearnedMethod{}, errors.New("an outcome needs the task session it came from")
	}
	if success {
		st, err := s.Read(ctx, taskSession)
		if err != nil {
			return LearnedMethod{}, err
		}
		if st.Work == nil {
			return LearnedMethod{}, errors.New("no work contract recorded for this task; a success needs verified requirements")
		}
		user := 0
		for _, item := range st.Work.Items {
			if item.Origin != OriginUser || item.Status == ItemSuperseded || item.Status == ItemDeclined {
				continue
			}
			user++
			if status := EffectiveStatus(item, st.Generation); status != ItemVerified && status != ItemDelivered {
				return LearnedMethod{}, fmt.Errorf("requirement %s is %s; a success needs every user requirement verified with current evidence", item.ID, status)
			}
		}
		if user == 0 {
			return LearnedMethod{}, errors.New("no user requirements recorded for this task; a success needs verified requirements")
		}
		completion, err := s.CompletionStatus(ctx, taskSession)
		if err != nil {
			return LearnedMethod{}, err
		}
		if completion == nil || completion.Verdict != "complete" {
			return LearnedMethod{}, errors.New("a successful method outcome requires a current aggregate completion decision, including review and external effects")
		}
		if len(taskFingerprint) != 1 || taskFingerprint[0] == "" || taskFingerprint[0] != completion.TasksFingerprint {
			return LearnedMethod{}, errors.New("a successful method outcome requires the current task-service fingerprint bound to the completion decision")
		}
	}
	k, err := methodKey(root)
	if err != nil {
		return LearnedMethod{}, err
	}
	var out LearnedMethod
	err = s.Update(ctx, k, func(st *State) error {
		i := slices.IndexFunc(st.LearnedMethods, func(m LearnedMethod) bool { return m.ID == methodID })
		if i < 0 {
			return errors.New("unknown learned method")
		}
		m := &st.LearnedMethods[i]
		m.Outcomes = append(m.Outcomes, MethodOutcome{TaskID: taskSession, Success: success, At: time.Now().UnixMilli()})
		if len(m.Outcomes) > 64 {
			m.Outcomes = m.Outcomes[len(m.Outcomes)-64:]
		}
		out = *m
		return nil
	})
	return out, err
}

// MethodChoice is one learned method as offered for reuse: how much to trust
// it and what to do about that.
type MethodChoice struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Standing    string `json:"standing"`
	Basis       string `json:"basis"`
	Guidance    string `json:"guidance"`
}

// ReusableMethods ranks the methods worth offering for the current task. A
// method that proved itself comes first and is recommended; a candidate is
// offered after it with a warning that it is unmeasured; an unreliable one is
// not offered at all. Within a standing the most recently learned comes first.
func ReusableMethods(methods []LearnedMethod, limit int) []MethodChoice {
	var effective, candidates []MethodChoice
	for i := len(methods) - 1; i >= 0; i-- {
		m := methods[i]
		standing, basis := MethodStanding(m)
		choice := MethodChoice{ID: m.ID, Name: m.Name, Description: m.Description, Status: m.Status, Standing: standing, Basis: basis}
		switch standing {
		case StandingEffective:
			choice.Guidance = "Succeeded on other tasks. Recheck its conditions and versions with workflow learn inspect, then prefer it over improvising."
			effective = append(effective, choice)
		case StandingCandidate:
			choice.Guidance = "Not yet measured beyond the task it came from. Use only if nothing better fits, verify every step, and report the outcome so it can be measured."
			candidates = append(candidates, choice)
		}
	}
	out := append(effective, candidates...)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}
