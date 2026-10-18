// Package cart prices shopping carts. All money is in integer cents and all
// rates are in basis points (1500 is 15.00%).
package cart

// Item is one cart line.
type Item struct {
	SKU       string
	Price     Cents // unit price
	Qty       int
	TaxExempt bool
}

// Cart is a priced basket with at most one coupon of each kind.
type Cart struct {
	Items         []Item
	PercentOffBps int   // percent coupon, applied to the subtotal
	AmountOff     Cents // fixed coupon
	TaxBps        int   // tax rate for items that are not TaxExempt
}

// Totals is the result of pricing a cart.
type Totals struct {
	Subtotal Cents
	Discount Cents
	Tax      Cents
	Total    Cents
}

// Totals prices the cart by these rules, in this order:
//
//  1. Subtotal is the sum of Price*Qty over all items.
//  2. The percent coupon takes PercentOffBps of the subtotal, rounded with
//     mulDivRound. It is computed first, on the full subtotal.
//  3. The fixed coupon AmountOff is added to that. The combined discount can
//     never exceed the subtotal, so a total never goes negative.
//  4. Tax applies only to items that are not TaxExempt. The discount is shared
//     with them pro rata: their share is the discount times their subtotal over
//     the full subtotal, rounded with mulDivRound. Tax is TaxBps of the taxable
//     subtotal minus that share, rounded with mulDivRound.
//  5. Total is Subtotal - Discount + Tax.
func (c Cart) Totals() Totals {
	var t Totals
	for _, it := range c.Items {
		t.Subtotal += it.Price * Cents(it.Qty)
	}
	afterFixed := t.Subtotal - c.AmountOff
	if afterFixed < 0 {
		afterFixed = 0
	}
	percent := mulDivRound(afterFixed, Cents(c.PercentOffBps), 10000)
	t.Discount = t.Subtotal - afterFixed + percent
	t.Tax = c.tax(t.Discount)
	t.Total = t.Subtotal - t.Discount + t.Tax
	return t
}
