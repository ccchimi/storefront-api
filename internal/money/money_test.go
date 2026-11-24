package money

import "testing"

func TestString(t *testing.T) {
	cases := map[Cents]string{0: "0.00", 5: "0.05", 1999: "19.99", 5997: "59.97", -250: "-2.50"}
	for in, want := range cases {
		if got := in.String(); got != want {
			t.Errorf("Cents(%d).String() = %q, want %q", in, got, want)
		}
	}
}

func TestFromFloatRoundsInsteadOfTruncating(t *testing.T) {
	if got := FromFloat(19.99); got != 1999 {
		t.Fatalf("FromFloat(19.99) = %d, want 1999", got)
	}
	if got := FromFloat(19.99).Mul(3); got != 5997 {
		t.Fatalf("3 x 19.99 = %d cents, want 5997", got)
	}
}
