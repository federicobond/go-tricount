package tricount

import (
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Amount is an exact decimal money value in a single currency.
//
// Amount is immutable: every method returns a new value and none mutates
// its receiver, so copying an Amount is safe. Amount is NOT comparable
// with == and must not be used as a map key; use Equal instead.
//
// The zero Amount is zero in an empty currency. Arithmetic between
// different currencies, the empty one included, is an error.
type Amount struct {
	currency string
	val      *big.Rat // nil means zero
}

var (
	scaleMu sync.RWMutex

	// currencyScale holds the ISO 4217 minor-unit counts that differ from 2.
	// RegisterCurrencyScale adds to it.
	currencyScale = map[string]int{
		"BIF": 0, "CLP": 0, "DJF": 0, "GNF": 0, "ISK": 0, "JPY": 0,
		"KMF": 0, "KRW": 0, "PYG": 0, "RWF": 0, "UGX": 0, "UYI": 0,
		"VND": 0, "VUV": 0, "XAF": 0, "XOF": 0, "XPF": 0,

		"BHD": 3, "IQD": 3, "JOD": 3, "KWD": 3, "LYD": 3, "OMR": 3, "TND": 3,
	}
)

// scaleOf reports how many decimal places a currency uses. Currencies absent
// from the table use 2, which is right for the large majority.
func scaleOf(currency string) int {
	scaleMu.RLock()
	defer scaleMu.RUnlock()
	if n, ok := currencyScale[currency]; ok {
		return n
	}
	return 2
}

// RegisterCurrencyScale teaches the package how many decimal places a currency
// uses, for the rare currency whose minor-unit count is neither the default of
// 2 nor in the built-in table. The number_of_decimal field of an ExchangeRates
// response is one place to find it.
//
// Call it during start-up, before Amounts in that currency are created or
// formatted concurrently.
func RegisterCurrencyScale(currency string, decimals int) error {
	if currency == "" {
		return fmt.Errorf("%w: empty currency", ErrInvalidRequest)
	}
	if decimals < 0 || decimals > 8 {
		return fmt.Errorf("%w: %d decimal places is out of range for %s",
			ErrInvalidRequest, decimals, currency)
	}
	scaleMu.Lock()
	defer scaleMu.Unlock()
	currencyScale[strings.ToUpper(currency)] = decimals
	return nil
}

var decimalRe = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)

// ParseAmount parses a decimal string such as "25.50" into an Amount. It
// rejects values carrying more decimal places than the currency has minor
// units, so ParseAmount("507.5", "JPY") is an error.
func ParseAmount(s, currency string) (Amount, error) {
	a, err := parseAmount(s, currency)
	if err != nil {
		return Amount{}, err
	}
	if got, allowed := decimalsIn(s), scaleOf(a.currency); got > allowed {
		return Amount{}, fmt.Errorf("%w: amount %q has %d decimal places, %s allows %d",
			ErrInvalidRequest, s, got, a.currency, allowed)
	}
	return a, nil
}

// parseAmount is the lenient form used when decoding server responses,
// which are authoritative about their own precision.
func parseAmount(s, currency string) (Amount, error) {
	if currency == "" {
		return Amount{}, fmt.Errorf("%w: amount %q has no currency", ErrInvalidRequest, s)
	}
	if !decimalRe.MatchString(s) {
		return Amount{}, fmt.Errorf("%w: %q is not a decimal amount", ErrInvalidRequest, s)
	}
	val, ok := new(big.Rat).SetString(s)
	if !ok {
		return Amount{}, fmt.Errorf("%w: %q is not a decimal amount", ErrInvalidRequest, s)
	}
	return Amount{currency: strings.ToUpper(currency), val: val}, nil
}

func decimalsIn(s string) int {
	if i := strings.IndexByte(s, '.'); i >= 0 {
		return len(s) - i - 1
	}
	return 0
}

// MustParseAmount is ParseAmount for literals; it panics on error.
func MustParseAmount(s, currency string) Amount {
	a, err := ParseAmount(s, currency)
	if err != nil {
		panic(err)
	}
	return a
}

// ZeroAmount returns zero in the given currency.
func ZeroAmount(currency string) Amount {
	return Amount{currency: strings.ToUpper(currency), val: new(big.Rat)}
}

// rat returns the value, substituting zero for the nil zero value. The
// result is never mutated by Amount's methods.
func (a Amount) rat() *big.Rat {
	if a.val == nil {
		return new(big.Rat)
	}
	return a.val
}

// Currency reports the ISO 4217 code.
func (a Amount) Currency() string { return a.currency }

// String formats the amount at its currency's scale: "25.50" for EUR,
// "507" for JPY, "1.000" for BHD.
func (a Amount) String() string { return a.rat().FloatString(scaleOf(a.currency)) }

// Float64 returns the value as a float. It is lossy and intended for
// display and sorting only, never for arithmetic that will be sent back.
func (a Amount) Float64() float64 {
	f, _ := a.rat().Float64()
	return f
}

// IsZero reports whether the value is zero, regardless of currency.
func (a Amount) IsZero() bool { return a.rat().Sign() == 0 }

// Sign returns -1, 0 or +1.
func (a Amount) Sign() int { return a.rat().Sign() }

// Neg returns the negated amount.
func (a Amount) Neg() Amount {
	return Amount{currency: a.currency, val: new(big.Rat).Neg(a.rat())}
}

// Abs returns the absolute value.
func (a Amount) Abs() Amount {
	return Amount{currency: a.currency, val: new(big.Rat).Abs(a.rat())}
}

func (a Amount) sameCurrency(b Amount) error {
	if a.currency != b.currency {
		return fmt.Errorf("%w: currency mismatch between %q and %q",
			ErrInvalidRequest, a.currency, b.currency)
	}
	if a.currency == "" {
		return fmt.Errorf("%w: amount has no currency", ErrInvalidRequest)
	}
	return nil
}

// Add returns a+b, erroring unless both share a non-empty currency.
func (a Amount) Add(b Amount) (Amount, error) {
	if err := a.sameCurrency(b); err != nil {
		return Amount{}, err
	}
	return Amount{currency: a.currency, val: new(big.Rat).Add(a.rat(), b.rat())}, nil
}

// Sub returns a-b, erroring unless both share a non-empty currency.
func (a Amount) Sub(b Amount) (Amount, error) {
	if err := a.sameCurrency(b); err != nil {
		return Amount{}, err
	}
	return Amount{currency: a.currency, val: new(big.Rat).Sub(a.rat(), b.rat())}, nil
}

// Cmp returns -1, 0 or +1 as a is less than, equal to, or greater than b.
func (a Amount) Cmp(b Amount) (int, error) {
	if err := a.sameCurrency(b); err != nil {
		return 0, err
	}
	return a.rat().Cmp(b.rat()), nil
}

// Equal reports whether both amounts are the same value in the same
// currency. Use it instead of ==, which does not work on Amount.
func (a Amount) Equal(b Amount) bool {
	return a.currency == b.currency && a.rat().Cmp(b.rat()) == 0
}

// DivideEqually splits the amount into n parts that sum exactly to it.
// Remainder minor units go to the earliest parts, so EUR 10.00 across
// three becomes 3.34, 3.33, 3.33.
func (a Amount) DivideEqually(n int) ([]Amount, error) {
	if n <= 0 {
		return nil, fmt.Errorf("%w: cannot divide into %d parts", ErrInvalidRequest, n)
	}
	shares := make([]int, n)
	for i := range shares {
		shares[i] = 1
	}
	return a.DivideByShares(shares)
}

// DivideByShares splits the amount in proportion to shares, summing exactly
// to the original. Leftover minor units go to the parts with the largest
// fractional remainder, ties broken toward earlier parts. This matches the
// API's own behaviour, where the extra unit of an odd split consistently
// lands on the same member.
func (a Amount) DivideByShares(shares []int) ([]Amount, error) {
	if len(shares) == 0 {
		return nil, fmt.Errorf("%w: no shares to divide between", ErrInvalidRequest)
	}
	weights := make([]*big.Int, len(shares))
	for i, s := range shares {
		if s <= 0 {
			return nil, fmt.Errorf("%w: share %d is %d, must be positive", ErrInvalidRequest, i, s)
		}
		weights[i] = big.NewInt(int64(s))
	}
	return a.divideByWeights(weights)
}

// divideByWeights is DivideByShares without the positivity requirement: a
// zero weight yields a zero part and takes no share of the remainder. It is
// how an exact split in one currency is mirrored into another.
func (a Amount) divideByWeights(weights []*big.Int) ([]Amount, error) {
	if len(weights) == 0 {
		return nil, fmt.Errorf("%w: no weights to divide between", ErrInvalidRequest)
	}
	totalWeight := new(big.Int)
	for i, wt := range weights {
		if wt == nil || wt.Sign() < 0 {
			return nil, fmt.Errorf("%w: weight %d is negative", ErrInvalidRequest, i)
		}
		totalWeight.Add(totalWeight, wt)
	}
	if totalWeight.Sign() == 0 {
		return nil, fmt.Errorf("%w: all weights are zero", ErrInvalidRequest)
	}

	units, err := a.units()
	if err != nil {
		return nil, err
	}
	negative := units.Sign() < 0
	units = new(big.Int).Abs(units)

	parts := make([]*big.Int, len(weights))
	rems := make([]*big.Int, len(weights))
	assigned := new(big.Int)
	for i, wt := range weights {
		num := new(big.Int).Mul(units, wt)
		q, r := new(big.Int).QuoRem(num, totalWeight, new(big.Int))
		parts[i], rems[i] = q, r
		assigned.Add(assigned, q)
	}

	// Each part loses less than one unit to truncation, so the leftover is
	// always smaller than the number of parts.
	leftover := new(big.Int).Sub(units, assigned)
	order := make([]int, len(weights))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(x, y int) bool {
		return rems[order[x]].Cmp(rems[order[y]]) > 0
	})
	one := big.NewInt(1)
	for i := 0; leftover.Sign() > 0 && i < len(order); i++ {
		parts[order[i]].Add(parts[order[i]], one)
		leftover.Sub(leftover, one)
	}

	out := make([]Amount, len(weights))
	for i, p := range parts {
		if negative {
			p = new(big.Int).Neg(p)
		}
		out[i] = a.fromUnits(p)
	}
	return out, nil
}

// units expresses the amount in its currency's minor units.
func (a Amount) units() (*big.Int, error) {
	scaled := new(big.Rat).Mul(a.rat(), ratPow10(scaleOf(a.currency)))
	if !scaled.IsInt() {
		return nil, fmt.Errorf("%w: %s is not representable in %s minor units",
			ErrInvalidRequest, a, a.currency)
	}
	return new(big.Int).Set(scaled.Num()), nil
}

// fromUnits is the inverse of units, in the receiver's currency.
func (a Amount) fromUnits(units *big.Int) Amount {
	return Amount{
		currency: a.currency,
		val:      new(big.Rat).SetFrac(new(big.Int).Set(units), intPow10(scaleOf(a.currency))),
	}
}

func intPow10(n int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
}

func ratPow10(n int) *big.Rat {
	return new(big.Rat).SetInt(intPow10(n))
}

// Convert applies an exchange rate and returns the value in toCurrency,
// rounded to that currency's scale. rate is a decimal string of the kind
// ExchangeRate returns.
func (a Amount) Convert(rate, toCurrency string) (Amount, error) {
	if toCurrency == "" {
		return Amount{}, fmt.Errorf("%w: no target currency for conversion", ErrInvalidRequest)
	}
	r, ok := new(big.Rat).SetString(rate)
	if !ok || r.Sign() <= 0 {
		return Amount{}, fmt.Errorf("%w: %q is not a positive exchange rate", ErrInvalidRequest, rate)
	}
	to := strings.ToUpper(toCurrency)
	product := new(big.Rat).Mul(a.rat(), r)
	return parseAmount(product.FloatString(scaleOf(to)), to)
}

// wireAmount is the API's amount object, {"value": "-25.50", "currency": "EUR"}.
type wireAmount struct {
	Value    string `json:"value"`
	Currency string `json:"currency"`
}

// MarshalJSON encodes the amount as the API's amount object, sign included.
// Flipping signs for the API's negative-expense convention is the wire
// layer's job, not this method's.
func (a Amount) MarshalJSON() ([]byte, error) {
	if a.currency == "" {
		return nil, fmt.Errorf("%w: cannot encode an amount with no currency", ErrInvalidRequest)
	}
	return json.Marshal(wireAmount{Value: a.String(), Currency: a.currency})
}

// UnmarshalJSON decodes the API's amount object. It is deliberately more
// lenient than ParseAmount about precision, because the server is
// authoritative about the values it sends.
func (a *Amount) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*a = Amount{}
		return nil
	}
	var w wireAmount
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	parsed, err := parseAmount(w.Value, w.Currency)
	if err != nil {
		return err
	}
	*a = parsed
	return nil
}
