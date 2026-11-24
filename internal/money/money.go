// Package money represents currency amounts as integer cents.
//
// Floating point is never used for arithmetic: 19.99 * 3 has to be exactly
// 59.97, and float64 can't promise that.
package money

import (
	"fmt"
	"math"
)

// Cents is an amount of money in the smallest currency unit.
type Cents int64

// Mul returns the amount multiplied by a quantity.
func (c Cents) Mul(qty int) Cents { return c * Cents(qty) }

// String formats the amount as a plain decimal, e.g. 5997 -> "59.97".
func (c Cents) String() string {
	sign := ""
	v := int64(c)
	if v < 0 {
		sign = "-"
		v = -v
	}
	return fmt.Sprintf("%s%d.%02d", sign, v/100, v%100)
}

// FromFloat converts a decimal amount to cents, rounding to the nearest cent.
// It exists for inputs that already arrive as floats; a plain int64(f*100)
// would turn 19.99 into 1998.
func FromFloat(f float64) Cents { return Cents(math.Round(f * 100)) }
