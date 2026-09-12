package tricount

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

const exchangeResponse = `{"Response":[
	{"ExchangeRate":{"currency_source":"USD","currency_target":"JPY","rate":"150.25",
	  "description":"Japanese Yen","number_of_decimal":0,"symbol":"¥"}},
	{"ExchangeRate":{"currency_source":"USD","currency_target":"EUR","rate":"0.92",
	  "description":"Euro","number_of_decimal":2,"symbol":"€"}}
]}`

func TestExchangeRates(t *testing.T) {
	var gotQuery string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(exchangeResponse))
	})

	rates, err := c.ExchangeRates(context.Background(), "USD")
	if err != nil {
		t.Fatalf("ExchangeRates: %v", err)
	}
	if gotQuery != "currency=USD" {
		t.Errorf("query = %q", gotQuery)
	}
	if rates["JPY"] != "150.25" {
		t.Errorf("JPY rate = %q, want \"150.25\"", rates["JPY"])
	}
	if rates["EUR"] != "0.92" {
		t.Errorf("EUR rate = %q, want \"0.92\"", rates["EUR"])
	}

	got, err := MustParseAmount("100.00", "USD").Convert(rates["EUR"], "EUR")
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if got.String() != "92.00" {
		t.Errorf("100 USD at 0.92 = %q, want \"92.00\"", got.String())
	}
}

func TestExchangeRate(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(exchangeResponse))
	})

	rate, err := c.ExchangeRate(context.Background(), "USD", "JPY")
	if err != nil {
		t.Fatalf("ExchangeRate: %v", err)
	}
	if rate != "150.25" {
		t.Errorf("rate = %q, want \"150.25\"", rate)
	}

	if _, err := c.ExchangeRate(context.Background(), "USD", "XYZ"); !errors.Is(err, ErrNotFound) {
		t.Errorf("an unlisted target = %v, want ErrNotFound", err)
	}
}

func TestExchangeRateSameCurrencyIsOne(t *testing.T) {
	var called bool
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Write([]byte(exchangeResponse))
	})
	rate, err := c.ExchangeRate(context.Background(), "EUR", "EUR")
	if err != nil {
		t.Fatalf("ExchangeRate: %v", err)
	}
	if rate != "1" {
		t.Errorf("rate = %q, want \"1\"", rate)
	}
	if called {
		t.Error("converting a currency to itself should not hit the network")
	}
}

func TestExchangeRatesValidation(t *testing.T) {
	var called bool
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	if _, err := c.ExchangeRates(context.Background(), ""); !errors.Is(err, ErrInvalidRequest) {
		t.Error("an empty source currency should be rejected")
	}
	if called {
		t.Error("validation failures must not reach the network")
	}
}

func TestRegisterCurrencyScale(t *testing.T) {
	if err := RegisterCurrencyScale("ZZZ", 4); err != nil {
		t.Fatalf("RegisterCurrencyScale: %v", err)
	}
	a, err := ParseAmount("1.2345", "ZZZ")
	if err != nil {
		t.Fatalf("ParseAmount after registering: %v", err)
	}
	if got := a.String(); got != "1.2345" {
		t.Errorf("String = %q, want \"1.2345\"", got)
	}
	if err := RegisterCurrencyScale("", 2); !errors.Is(err, ErrInvalidRequest) {
		t.Error("an empty currency should be rejected")
	}
	if err := RegisterCurrencyScale("ZZY", -1); !errors.Is(err, ErrInvalidRequest) {
		t.Error("a negative scale should be rejected")
	}
}
