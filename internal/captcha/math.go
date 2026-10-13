	}
	for {
		op := p.peekOp('*', '/')
		if op == 0 {
			return v, nil
		}
		p.pos++
		rhs, err := p.parseFactor()
		if err != nil {
			return 0, err
		}
		if op == '*' {
			v *= rhs
		} else {
			if rhs == 0 {
				return 0, fmt.Errorf("captcha: division by zero")
			}
			v /= rhs
		}
	}
}

func (p *mathParser) parseFactor() (float64, error) {
	if p.peekOp('(', '(') == '(' {
		p.pos++
		v, err := p.parseExpr()
		if err != nil {
			return 0, err
		}
		if p.peekOp(')', ')') != ')' {
			return 0, fmt.Errorf("captcha: missing closing parenthesis")
		}
		p.pos++
		return v, nil
	}
	if p.peekOp('-', '-') == '-' {
		p.pos++
		v, err := p.parseFactor()
		if err != nil {
			return 0, err
		}
		return -v, nil
	}
	if p.peekOp('+', '+') == '+' {
		p.pos++
		return p.parseFactor()
	}
	start := p.pos
	for p.pos < len(p.s) && (p.s[p.pos] == '.' || (p.s[p.pos] >= '0' && p.s[p.pos] <= '9')) {
		p.pos++
	}
	if start == p.pos {
		return 0, fmt.Errorf("captcha: expected number at %q", p.s[p.pos:])
	}
	n, err := strconv.ParseFloat(p.s[start:p.pos], 64)
	if err != nil {
		return 0, fmt.Errorf("captcha: bad number %q", p.s[start:p.pos])
	}
	return n, nil
}

func (p *mathParser) peekOp(ops ...byte) byte {
	if p.pos >= len(p.s) {
		return 0
	}
	c := p.s[p.pos]
	for _, op := range ops {
		if c == op {
			return c
		}
	}
	return 0
}
