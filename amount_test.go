package tricount

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestParseAmountString(t *testing.T) {
	cases := []struct{ in, currency, want string }{
		{"25.50", "EUR", "25.50"},
		{"25.5", "EUR", "25.50"},
		{"25", "EUR", "25.00"},
		{"-25.50", "EUR", "-25.50"},
		{"0", "EUR", "0.00"},
		{"507", "JPY", "507"},
		{"-507", "JPY", "-507"},
		{"1.234", "BHD", "1.234"},
		{"1", "BHD", "1.000"},
		{"12345678901234567890.99", "EUR", "12345678901234567890.99"},
	}
	for _, c := range cases {
		a, err := ParseAmount(c.in, c.currency)
		if err != nil {
			t.Fatalf("ParseAmount(%q, %q): %v", c.in, c.currency, err)
		}
		if got := a.String(); got != c.want {
			t.Errorf("ParseAmount(%q, %q).String() = %q, want %q", c.in, c.currency, got, c.want)
		}
		if got := a.Currency(); got != c.currency {
			t.Errorf("ParseAmount(%q, %q).Currency() = %q, want %q", c.in, c.currency, got, c.currency)
		}
	}
}

func TestParseAmountRejects(t *testing.T) {
	cases := []struct{ in, currency, why string }{
		{"25.555", "EUR", "more precision than EUR allows"},
		{"507.5", "JPY", "JPY has no minor units"},
		{"", "EUR", "empty string"},
		{"abc", "EUR", "not a number"},
		{"1/3", "EUR", "big.Rat rational syntax"},
		{"1e3", "EUR", "exponent syntax"},
		{" 25.50", "EUR", "leading space"},
		{"+25.50", "EUR", "explicit plus"},
		{"25.50", "", "empty currency"},
	}
	for _, c := range cases {
		_, err := ParseAmount(c.in, c.currency)
		if err == nil {
			t.Errorf("ParseAmount(%q, %q) succeeded, want error (%s)", c.in, c.currency, c.why)
			continue
		}
		if !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("ParseAmount(%q, %q) = %v, want ErrInvalidRequest", c.in, c.currency, err)
		}
	}
}

func TestAmountArithmetic(t *testing.T) {
	a := MustParseAmount("10.00", "EUR")
	b := MustParseAmount("2.50", "EUR")

	sum, err := a.Add(b)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got := sum.String(); got != "12.50" {
		t.Errorf("10.00 + 2.50 = %q, want \"12.50\"", got)
	}

	diff, err := a.Sub(b)
	if err != nil {
		t.Fatalf("Sub: %v", err)
	}
	if got := diff.String(); got != "7.50" {
		t.Errorf("10.00 - 2.50 = %q, want \"7.50\"", got)
	}

	if got := a.String(); got != "10.00" {
		t.Errorf("arithmetic mutated the receiver: %q", got)
	}

	cmp, err := a.Cmp(b)
	if err != nil {
		t.Fatalf("Cmp: %v", err)
	}
	if cmp != 1 {
		t.Errorf("Cmp = %d, want 1", cmp)
	}

	if !a.Equal(MustParseAmount("10.00", "EUR")) {
		t.Error("equal amounts not reported equal")
	}
	if a.Equal(MustParseAmount("10", "JPY")) {
		t.Error("amounts in different currencies reported equal")
	}
	if got := a.Neg().String(); got != "-10.00" {
		t.Errorf("Neg = %q, want \"-10.00\"", got)
	}
	if got := a.Neg().Abs().String(); got != "10.00" {
		t.Errorf("Abs = %q, want \"10.00\"", got)
	}
	if !ZeroAmount("EUR").IsZero() {
		t.Error("ZeroAmount is not zero")
	}
	if a.Sign() != 1 || a.Neg().Sign() != -1 || ZeroAmount("EUR").Sign() != 0 {
		t.Error("Sign is wrong")
	}
	if _, err := a.Add(MustParseAmount("1", "JPY")); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("cross-currency Add = %v, want ErrInvalidRequest", err)
	}
}

func TestAmountValueSemantics(t *testing.T) {
	a := MustParseAmount("10.00", "EUR")
	b := a // a copy shares the underlying *big.Rat

	_, _ = a.Add(MustParseAmount("5.00", "EUR"))
	_, _ = a.Sub(MustParseAmount("5.00", "EUR"))
	_ = a.Neg()
	_ = a.Abs()

	if got := b.String(); got != "10.00" {
		t.Fatalf("operations mutated a shared value: %q", got)
	}
}

func TestZeroValueAmount(t *testing.T) {
	var a Amount
	if !a.IsZero() {
		t.Error("zero-value Amount is not zero")
	}
	if got := a.String(); got != "0.00" {
		t.Errorf("zero-value String = %q, want \"0.00\"", got)
	}
	if _, err := a.Add(MustParseAmount("1.00", "EUR")); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("adding to a currency-less amount = %v, want ErrInvalidRequest", err)
	}
}

func TestDivideEqually(t *testing.T) {
	cases := []struct {
		amount, currency string
		n                int
		want             []string
	}{
		{"10.00", "EUR", 3, []string{"3.34", "3.33", "3.33"}},
		{"10.00", "EUR", 2, []string{"5.00", "5.00"}},
		{"10.00", "EUR", 1, []string{"10.00"}},
		{"120.00", "EUR", 7, []string{"17.15", "17.15", "17.14", "17.14", "17.14", "17.14", "17.14"}},
		{"507", "JPY", 2, []string{"254", "253"}},
		{"0.01", "EUR", 3, []string{"0.01", "0.00", "0.00"}},
		{"1.000", "BHD", 3, []string{"0.334", "0.333", "0.333"}},
		{"-10.00", "EUR", 3, []string{"-3.34", "-3.33", "-3.33"}},
	}
	for _, c := range cases {
		total := MustParseAmount(c.amount, c.currency)
		parts, err := total.DivideEqually(c.n)
		if err != nil {
			t.Fatalf("%s %s / %d: %v", c.amount, c.currency, c.n, err)
		}
		got := make([]string, len(parts))
		sum := ZeroAmount(c.currency)
		for i, p := range parts {
			got[i] = p.String()
			sum, err = sum.Add(p)
			if err != nil {
				t.Fatalf("summing parts: %v", err)
			}
		}
		if len(got) != len(c.want) {
			t.Fatalf("%s %s / %d: got %d parts, want %d", c.amount, c.currency, c.n, len(got), len(c.want))
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s %s / %d: part %d = %q, want %q", c.amount, c.currency, c.n, i, got[i], c.want[i])
			}
		}
		if !sum.Equal(total) {
			t.Errorf("%s %s / %d: parts sum to %s, want %s", c.amount, c.currency, c.n, sum, total)
		}
	}
}

func TestDivideByShares(t *testing.T) {
	cases := []struct {
		amount, currency string
		shares           []int
		want             []string
	}{
		{"100.00", "EUR", []int{1, 2, 1}, []string{"25.00", "50.00", "25.00"}},
		{"100.00", "EUR", []int{1, 1, 1}, []string{"33.34", "33.33", "33.33"}},
		{"10.00", "EUR", []int{2, 1}, []string{"6.67", "3.33"}},
		{"300.00", "EUR", []int{2, 1}, []string{"200.00", "100.00"}},
		{"507", "JPY", []int{1, 1, 1}, []string{"169", "169", "169"}},
	}
	for _, c := range cases {
		total := MustParseAmount(c.amount, c.currency)
		parts, err := total.DivideByShares(c.shares)
		if err != nil {
			t.Fatalf("%s %s by %v: %v", c.amount, c.currency, c.shares, err)
		}
		sum := ZeroAmount(c.currency)
		for i, p := range parts {
			if got := p.String(); got != c.want[i] {
				t.Errorf("%s %s by %v: part %d = %q, want %q", c.amount, c.currency, c.shares, i, got, c.want[i])
			}
			sum, _ = sum.Add(p)
		}
		if !sum.Equal(total) {
			t.Errorf("%s %s by %v: parts sum to %s, want %s", c.amount, c.currency, c.shares, sum, total)
		}
	}
}

func TestDivideRejects(t *testing.T) {
	a := MustParseAmount("10.00", "EUR")
	if _, err := a.DivideEqually(0); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("DivideEqually(0) = %v, want ErrInvalidRequest", err)
	}
	if _, err := a.DivideEqually(-1); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("DivideEqually(-1) = %v, want ErrInvalidRequest", err)
	}
	if _, err := a.DivideByShares(nil); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("DivideByShares(nil) = %v, want ErrInvalidRequest", err)
	}
	if _, err := a.DivideByShares([]int{1, 0}); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("DivideByShares with a zero share = %v, want ErrInvalidRequest", err)
	}
	if _, err := a.DivideByShares([]int{1, -2}); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("DivideByShares with a negative share = %v, want ErrInvalidRequest", err)
	}
}

func TestConvert(t *testing.T) {
	cases := []struct{ from, currency, rate, to, want string }{
		{"100.00", "USD", "150", "JPY", "15000"},
		{"100.00", "USD", "0.9231", "EUR", "92.31"},
		{"1.00", "EUR", "1.005", "EUR", "1.01"},
		{"15000", "JPY", "0.0067", "USD", "100.50"},
	}
	for _, c := range cases {
		got, err := MustParseAmount(c.from, c.currency).Convert(c.rate, c.to)
		if err != nil {
			t.Fatalf("%s %s * %s: %v", c.from, c.currency, c.rate, err)
		}
		if got.String() != c.want {
			t.Errorf("%s %s * %s = %q, want %q", c.from, c.currency, c.rate, got.String(), c.want)
		}
		if got.Currency() != c.to {
			t.Errorf("converted currency = %q, want %q", got.Currency(), c.to)
		}
	}
	if _, err := MustParseAmount("1.00", "EUR").Convert("0", "JPY"); !errors.Is(err, ErrInvalidRequest) {
		t.Error("a zero rate should be rejected")
	}
	if _, err := MustParseAmount("1.00", "EUR").Convert("nope", "JPY"); !errors.Is(err, ErrInvalidRequest) {
		t.Error("a malformed rate should be rejected")
	}
}

func TestAmountJSON(t *testing.T) {
	a := MustParseAmount("-507", "JPY")
	b, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if got, want := string(b), `{"value":"-507","currency":"JPY"}`; got != want {
		t.Errorf("Marshal = %s, want %s", got, want)
	}

	var back Amount
	if err := json.Unmarshal([]byte(`{"value":"-395.00","currency":"EUR"}`), &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got := back.String(); got != "-395.00" {
		t.Errorf("round trip = %q, want \"-395.00\"", got)
	}
	if got := back.Currency(); got != "EUR" {
		t.Errorf("round-trip currency = %q, want \"EUR\"", got)
	}

	var loose Amount
	if err := json.Unmarshal([]byte(`{"value":"-25.555","currency":"EUR"}`), &loose); err != nil {
		t.Errorf("Unmarshal of excess precision should be lenient, got %v", err)
	}

	var null Amount
	if err := json.Unmarshal([]byte(`null`), &null); err != nil {
		t.Errorf("Unmarshal of null: %v", err)
	}
	if !null.IsZero() {
		t.Error("null should decode to the zero Amount")
	}

	if _, err := json.Marshal(Amount{}); err == nil {
		t.Error("marshalling a currency-less amount should fail")
	}
}
