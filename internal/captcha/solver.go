	return f.Accept(kind)
}

func (f FuncSolver) Solve(ctx context.Context, ch Challenge, image []byte) (Solution, error) {
	if f.Fn == nil {
		return Solution{}, errors.New("captcha: nil solver function")
	}
	return f.Fn(ctx, ch, image)
}
