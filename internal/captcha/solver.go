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
