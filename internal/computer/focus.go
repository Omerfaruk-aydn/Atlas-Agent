		}
		if err := check(); err != nil {
			return res, err
		}
		res.Tried = append(res.Tried, attempt.Method)
		if err := attempt.Run(); err != nil {
			continue
		}
		if ops.Foreground() || ops.Settle(ctx, settle) {
			if err := check(); err != nil {
				return res, err
			}
			res.Focused, res.Method = true, attempt.Method
			return res, nil
		}
		if err := ctx.Err(); err != nil {
			return res, err
		}
	}
	return res, fmt.Errorf("focus_denied: Windows did not activate window %s after %v; ask the person at the computer to click the window, then observe again before input", id, res.Tried)
}
