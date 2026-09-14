package money

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseDecimal reads an exact signed decimal in a currency's minor units.
// Blank input is zero. A leading sign and dot decimal separator are accepted;
// grouping and exponent notation are not. Excess fractional digits must be
// zero. Input rules such as price limits and decimal commas belong to callers.
func ParseDecimal(s, currency string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	negative := false
	switch s[0] {
	case '-':
		negative, s = true, s[1:]
	case '+':
		s = s[1:]
	}
	whole, fraction, _ := strings.Cut(s, ".")
	if whole == "" && fraction == "" {
		return 0, fmt.Errorf("money %q: no digits", s)
	}
	limit := uint64(1<<63 - 1)
	if negative {
		limit++
	}
	var value uint64
	appendDigit := func(digit uint64) bool {
		if value > (limit-digit)/10 {
			return false
		}
		value = value*10 + digit
		return true
	}
	for _, digit := range whole {
		if digit < '0' || digit > '9' {
			return 0, fmt.Errorf("money %q: bad digit %q", s, digit)
		}
		if !appendDigit(uint64(digit - '0')) {
			return 0, fmt.Errorf("money %q: overflow", s)
		}
	}
	units := MinorUnits(currency)
	for i, digit := range fraction {
		if digit < '0' || digit > '9' {
			return 0, fmt.Errorf("money %q: bad digit %q", s, digit)
		}
		if i < units {
			if !appendDigit(uint64(digit - '0')) {
				return 0, fmt.Errorf("money %q: overflow", s)
			}
		} else if digit != '0' {
			return 0, fmt.Errorf("money %q: fractional digits beyond exponent %d for %s", s, units, currency)
		}
	}
	for i := len(fraction); i < units; i++ {
		if !appendDigit(0) {
			return 0, fmt.Errorf("money %q: overflow", s)
		}
	}
	if negative {
		// Unsigned subtraction also handles the magnitude of MinInt64.
		return int64(-value), nil
	}
	return int64(value), nil
}

// Decimal formats minor units without grouping, using a dot decimal mark.
// All int64 values are supported, including MinInt64.
func Decimal(minor int64, currency string) string {
	units := MinorUnits(currency)
	if units == 0 {
		return strconv.FormatInt(minor, 10)
	}
	negative := minor < 0
	magnitude := uint64(minor)
	if negative {
		magnitude = -magnitude
	}
	digits := strconv.FormatUint(magnitude, 10)
	if len(digits) <= units {
		digits = strings.Repeat("0", units+1-len(digits)) + digits
	}
	split := len(digits) - units
	result := digits[:split] + "." + digits[split:]
	if negative {
		return "-" + result
	}
	return result
}
