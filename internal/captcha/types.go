}

// Attempt records one solver's try. The audit trail is kept for
// diagnostics and never contains challenge pixels.
type Attempt struct {
	Solver   string        `json:"solver"`
	Kind     Kind          `json:"kind"`
	OK       bool          `json:"ok"`
	Err      string        `json:"error,omitempty"`
	Duration time.Duration `json:"duration_ms"`
}

// Result is the outcome of running the chain against one challenge.
type Result struct {
	Solution Solution  `json:"solution,omitempty"`
	Attempts []Attempt `json:"attempts"`
	// Solved is true when a usable answer exists.
	Solved bool `json:"solved"`
	// FallbackCount is how many solvers failed before the winner.
	FallbackCount int `json:"fallback_count,omitempty"`
}

// Solver turns a challenge and its captured pixels into an answer.
// Implementations must not log or persist the image.
type Solver interface {
	// Name identifies the solver in audit records.
	Name() string
	// Supports reports whether this solver can handle the kind.
	Supports(kind Kind) bool
	// Solve answers the challenge. Image is nil for token kinds that
	// only need site metadata.
	Solve(ctx context.Context, ch Challenge, image []byte) (Solution, error)
}

// ErrNoSolver means no configured solver supports the challenge kind.
var ErrNoSolver = errors.New("captcha: no configured solver supports this challenge kind")

// ErrUnsolved means every solver failed or produced an empty answer.
var ErrUnsolved = errors.New("captcha: challenge unsolved")

// ErrUnsupportedKind means the challenge kind is not solvable by the
// current chain (for example a checkbox with no clicker wired).
var ErrUnsupportedKind = errors.New("captcha: unsupported challenge kind")

// SolverError annotates a solver failure with its backend name so the
// chain can record a useful attempt row.
type SolverError struct {
	Solver string
	Err    error
}

func (e *SolverError) Error() string {
	return fmt.Sprintf("captcha solver %s: %v", e.Solver, e.Err)
}

func (e *SolverError) Unwrap() error { return e.Err }

// IsEmpty reports whether a solution carries no usable answer.
func (s Solution) IsEmpty() bool {
	return s.Text == "" && s.Token == ""
}
