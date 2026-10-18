package cart

// taxableSubtotal is the subtotal of the items that are not TaxExempt.
func (c Cart) taxableSubtotal() Cents {
	var sum Cents
	for _, it := range c.Items {
		if !it.TaxExempt {
			sum += it.Price * Cents(it.Qty)
		}
	}
	return sum
}

// tax returns the tax owed on the cart given its subtotal and total discount.
// See Cart.Totals for the rules.
func (c Cart) tax(subtotal, discount Cents) Cents {
	taxable := c.taxableSubtotal()
	share := mulDivRound(discount, taxable, subtotal)
	return mulDivRound(taxable-share, Cents(c.TaxBps), 10000)
}
