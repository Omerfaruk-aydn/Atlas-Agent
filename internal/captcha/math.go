	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "x", "*")
	s = strings.ReplaceAll(s, "×", "*")
	s = strings.ReplaceAll(s, "÷", "/")
	s = strings.ReplaceAll(s, ":", "/")
	s = strings.ReplaceAll(s, "hesapla", " ")
	s = strings.ReplaceAll(s, "hesaplayın", " ")
	s = strings.ReplaceAll(s, "sonuc", " ")
	s = strings.ReplaceAll(s, "sonuç", " ")
	s = strings.ReplaceAll(s, "sonucu", " ")
	s = strings.ReplaceAll(s, "=", " ")
	s = strings.ReplaceAll(s, "?", " ")
	s = strings.ReplaceAll(s, "soru", " ")
	// Keep only arithmetic characters and digits.
	s = mathNoiseRe.ReplaceAllString(s, " ")
	// Insert explicit * for "2(3+4)" patterns.
	s = regexp.MustCompile(`(\d)\(`).ReplaceAllString(s, "$1*(")
	return strings.TrimSpace(s)
}

// EvalMath evaluates a left-to-right arithmetic expression with the four
// operators and parentheses. Division is float division. Unknown syntax
// is rejected instead of guessed.
func EvalMath(expr string) (float64, error) {
	expr = strings.ReplaceAll(expr, " ", "")
	if expr == "" {
		return 0, fmt.Errorf("captcha: empty math expression")
	}
	p := &mathParser{s: expr}
	v, err := p.parseExpr()
	if err != nil {
		return 0, err
	}
	if p.pos != len(p.s) {
		return 0, fmt.Errorf("captcha: trailing input in math expression %q", expr)
	}
	return v, nil
}

type mathParser struct {
	s   string
	pos int
}

func (p *mathParser) parseExpr() (float64, error) {
	v, err := p.parseTerm()
	if err != nil {
		return 0, err
	}
	for {
		op := p.peekOp('+', '-')
		if op == 0 {
			return v, nil
		}
		p.pos++
		rhs, err := p.parseTerm()
		if err != nil {
			return 0, err
		}
		if op == '+' {
			v += rhs
		} else {
			v -= rhs
		}
	}
}

func (p *mathParser) parseTerm() (float64, error) {
	v, err := p.parseFactor()
	if err != nil {
		return 0, err
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
