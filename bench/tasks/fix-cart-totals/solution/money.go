package cart

// Cents is an amount of money in whole cents.
type Cents int64

// mulDivRound returns a*b/c for non-negative a and b and positive c, rounded
// to the nearest whole cent with exact halves rounding up. It returns 0 when
// c is 0. The result must be exact: it may not go through floating point.
func mulDivRound(a, b, c Cents) Cents {
	if c == 0 {
		return 0
	}
	return (a*b + c/2) / c
}
