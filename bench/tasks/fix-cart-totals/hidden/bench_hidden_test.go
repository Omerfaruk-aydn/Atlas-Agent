package cart

import "testing"

func TestHiddenTotals(t *testing.T) {
	cases := []struct {
		name string
		cart Cart
		want Totals
	}{
		{
			name: "empty cart",
			cart: Cart{TaxBps: 825},
			want: Totals{},
		},
		{
			name: "tax skips exempt items",
			cart: Cart{Items: []Item{{Price: 1999, Qty: 1}, {Price: 500, Qty: 2, TaxExempt: true}}, TaxBps: 825},
			// subtotal 2999; tax 1999*825/10000 = 164.9175 -> 165
			want: Totals{Subtotal: 2999, Discount: 0, Tax: 165, Total: 3164},
		},
		{
			name: "percent coupon rounds to nearest cent",
			cart: Cart{Items: []Item{{Price: 999, Qty: 1}}, PercentOffBps: 1500},
			// 999*0.15 = 149.85 -> 150
			want: Totals{Subtotal: 999, Discount: 150, Total: 849},
		},
		{
			name: "exact half rounds up",
			cart: Cart{Items: []Item{{Price: 5, Qty: 1}}, PercentOffBps: 1000},
			// 5*0.10 = 0.5 -> 1
			want: Totals{Subtotal: 5, Discount: 1, Total: 4},
		},
		{
			name: "percent coupon comes before the fixed coupon",
			cart: Cart{Items: []Item{{Price: 10000, Qty: 1}}, PercentOffBps: 1000, AmountOff: 2000},
			// 10% of the full 10000 is 1000, plus 2000
			want: Totals{Subtotal: 10000, Discount: 3000, Total: 7000},
		},
		{
			name: "discount never exceeds the subtotal",
			cart: Cart{Items: []Item{{Price: 1500, Qty: 1}}, AmountOff: 5000, TaxBps: 1000},
			want: Totals{Subtotal: 1500, Discount: 1500, Tax: 0, Total: 0},
		},
		{
			name: "combined coupons are capped too",
			cart: Cart{Items: []Item{{Price: 1000, Qty: 1}}, PercentOffBps: 5000, AmountOff: 800},
			want: Totals{Subtotal: 1000, Discount: 1000, Total: 0},
		},
		{
			name: "discount is shared pro rata with taxable items",
			cart: Cart{Items: []Item{{Price: 6000, Qty: 1}, {Price: 4000, Qty: 1, TaxExempt: true}}, PercentOffBps: 2000, TaxBps: 1000},
			// discount 2000; taxable share 2000*6000/10000 = 1200; tax (6000-1200)*10% = 480
			want: Totals{Subtotal: 10000, Discount: 2000, Tax: 480, Total: 8480},
		},
		{
			name: "pro rata share is rounded",
			cart: Cart{Items: []Item{{Price: 333, Qty: 1}, {Price: 667, Qty: 1, TaxExempt: true}}, AmountOff: 100, TaxBps: 500},
			// share 100*333/1000 = 33.3 -> 33; tax (333-33)*5% = 15
			want: Totals{Subtotal: 1000, Discount: 100, Tax: 15, Total: 915},
		},
		{
			name: "quantities multiply",
			cart: Cart{Items: []Item{{Price: 250, Qty: 3}}, TaxBps: 1000},
			want: Totals{Subtotal: 750, Tax: 75, Total: 825},
		},
		{
			name: "large amounts stay exact",
			cart: Cart{Items: []Item{{Price: 123456789, Qty: 40}}, PercentOffBps: 1234},
			// subtotal 4938271560; 4938271560*1234/10000 = 609382710.504 -> 609382711
			want: Totals{Subtotal: 4938271560, Discount: 609382711, Total: 4328888849},
		},
	}
	for _, tc := range cases {
		if got := tc.cart.Totals(); got != tc.want {
			t.Errorf("%s: Totals = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}
