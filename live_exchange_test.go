//go:build live

package tricount

import (
	"context"
	"testing"
)

func TestLiveExchangeRates(t *testing.T) {
	c := liveClient(t)
	liveTricount(t) // ensures the device is set up

	rates, err := c.ExchangeRates(context.Background(), "EUR")
	if err != nil {
		t.Fatalf("ExchangeRates: %v", err)
	}
	if len(rates) == 0 {
		t.Fatal("the real API returned no exchange rates")
	}
	for _, target := range []string{"USD", "JPY"} {
		rate, ok := rates[target]
		if !ok {
			t.Errorf("no EUR to %s rate in %d rates", target, len(rates))
			continue
		}
		// Every rate must be usable by Convert, which means it must parse.
		if _, err := MustParseAmount("100.00", "EUR").Convert(rate, target); err != nil {
			t.Errorf("EUR to %s rate %q is not usable: %v", target, rate, err)
		}
	}
	t.Logf("the real API returned %d rates from EUR; USD=%s JPY=%s",
		len(rates), rates["USD"], rates["JPY"])

	single, err := c.ExchangeRate(context.Background(), "EUR", "USD")
	if err != nil {
		t.Fatalf("ExchangeRate: %v", err)
	}
	if single != rates["USD"] {
		t.Errorf("ExchangeRate gave %q but ExchangeRates gave %q", single, rates["USD"])
	}
}
