package captcha

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Chain runs solvers in priority order until one returns a usable answer.
// Built-in solvers (math) need no network and no API key. When every
// solver fails the caller falls back to showing the challenge image to
// the model, which can read it with its own vision.
type Chain struct {
	solvers []Solver
	// AttemptTimeout bounds a single solver try. Zero means the parent
	// context alone.
	AttemptTimeout time.Duration
	// Audit, when non-nil, receives one row per try.
	Audit func(Attempt)
}

// NewChain builds a chain from solvers in caller-preference order.
func NewChain(solvers ...Solver) *Chain {
	return &Chain{
		solvers:        append([]Solver(nil), solvers...),
		AttemptTimeout: 30 * time.Second,
	}
}

// WithAttemptTimeout overrides the per-solver deadline.
func (c *Chain) WithAttemptTimeout(d time.Duration) *Chain {
	if d > 0 {
		c.AttemptTimeout = d
	}
	return c
}

// WithAudit installs an attempt observer.
func (c *Chain) WithAudit(fn func(Attempt)) *Chain {
	c.Audit = fn
	return c
}

// Add appends a solver to the end of the chain.
func (c *Chain) Add(s Solver) {
	if s != nil {
		c.solvers = append(c.solvers, s)
	}
}

// Solvers returns the configured backend names in order.
func (c *Chain) Solvers() []string {
	names := make([]string, 0, len(c.solvers))
	for _, s := range c.solvers {
		names = append(names, s.Name())
	}
	return names
}

// Supports reports whether any solver can handle the kind.
func (c *Chain) Supports(kind Kind) bool {
	for _, s := range c.solvers {
		if s.Supports(kind) {
			return true
		}
	}
	return false
}

// Solve runs the chain against one challenge. Image may be nil for token
// kinds. The returned Result always carries the attempt list.
func (c *Chain) Solve(ctx context.Context, ch Challenge, image []byte) (Result, error) {
	res := Result{}
	if ch.Kind == KindCheckbox || ch.Kind == KindUnknown || ch.Kind == "" || ch.Kind == KindNone {
		return res, fmt.Errorf("%w: %s", ErrUnsupportedKind, ch.Kind)
	}
	candidates := make([]Solver, 0, len(c.solvers))
	for _, s := range c.solvers {
		if s.Supports(ch.Kind) {
			candidates = append(candidates, s)
		}
	}
	if len(candidates) == 0 {
		return res, ErrNoSolver
	}

	var lastErr error
	for _, s := range candidates {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		sol, att := c.try(ctx, s, ch, image)
		res.Attempts = append(res.Attempts, att)
		if c.Audit != nil {
			c.Audit(att)
		}
		if att.OK {
			sol.Solver = s.Name()
			if sol.Duration == 0 {
				sol.Duration = att.Duration
			}
			res.Solution = sol
			res.Solved = true
			return res, nil
		}
		lastErr = &SolverError{Solver: s.Name(), Err: errors.New(att.Err)}
	}
	res.FallbackCount = max(len(res.Attempts)-1, 0)
	if lastErr == nil {
		lastErr = ErrUnsolved
	}
	return res, lastErr
}

func (c *Chain) try(ctx context.Context, s Solver, ch Challenge, image []byte) (Solution, Attempt) {
	att := Attempt{Solver: s.Name(), Kind: ch.Kind}
	start := time.Now()
	tryCtx := ctx
	cancel := func() {}
	if c.AttemptTimeout > 0 {
		tryCtx, cancel = context.WithTimeout(ctx, c.AttemptTimeout)
	}
	defer cancel()
	sol, err := s.Solve(tryCtx, ch, image)
	att.Duration = time.Since(start)
	switch {
	case err != nil:
		att.Err = err.Error()
	case sol.IsEmpty():
		att.Err = ErrUnsolved.Error()
	default:
		att.OK = true
	}
	return sol, att
}

// FuncSolver adapts a function to Solver. Used by tests and by one-off
// backends.
type FuncSolver struct {
	NameStr string
	Accept  func(Kind) bool
	Fn      func(ctx context.Context, ch Challenge, image []byte) (Solution, error)
}

func (f FuncSolver) Name() string { return f.NameStr }

func (f FuncSolver) Supports(kind Kind) bool {
	if f.Accept == nil {
		return true
	}
	return f.Accept(kind)
}

func (f FuncSolver) Solve(ctx context.Context, ch Challenge, image []byte) (Solution, error) {
	if f.Fn == nil {
		return Solution{}, errors.New("captcha: nil solver function")
	}
	return f.Fn(ctx, ch, image)
}
