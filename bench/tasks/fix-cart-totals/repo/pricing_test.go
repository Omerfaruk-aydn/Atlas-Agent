package cart

import "testing"

func TestSubtotalAndPlainTax(t *testing.T) {
	c := Cart{Items: []Item{{SKU: "a", Price: 1000, Qty: 2}}, TaxBps: 1000}
	got := c.Totals()
	if got.Subtotal != 2000 || got.Tax != 200 || got.Total != 2200 {
		t.Fatalf("Totals = %+v", got)
	}
}
