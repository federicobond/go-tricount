package tricount

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// wireExchangeRate is one entry of a GET /exchange-rate response.
type wireExchangeRate struct {
	CurrencySource  string `json:"currency_source"`
	CurrencyTarget  string `json:"currency_target"`
	Rate            string `json:"rate"`
	Description     string `json:"description"`
	NumberOfDecimal int    `json:"number_of_decimal"`
	Symbol          string `json:"symbol"`
}

// ExchangeRates returns the rate from one currency to every currency the API
// supports, keyed by target code. Rates are decimal strings, so they feed
// Amount.Convert without a float ever being involved: one unit of from equals
// rate units of the target.
func (c *Client) ExchangeRates(ctx context.Context, from string) (map[string]string, error) {
	if from == "" {
		return nil, fmt.Errorf("%w: empty source currency", ErrInvalidRequest)
	}
	body, err := c.do(ctx, request{
		method:   http.MethodGet,
		userPath: "/exchange-rate",
		query:    url.Values{"currency": {strings.ToUpper(from)}},
	})
	if err != nil {
		return nil, err
	}
	wires, err := decodeEnvelope[wireExchangeRate](body, "ExchangeRate")
	if err != nil {
		return nil, err
	}
	rates := make(map[string]string, len(wires))
	for _, w := range wires {
		rates[w.CurrencyTarget] = w.Rate
	}
	return rates, nil
}

// ExchangeRate returns the rate between two currencies, as a decimal string.
// Converting a currency to itself returns "1" without a request.
func (c *Client) ExchangeRate(ctx context.Context, from, to string) (string, error) {
	from, to = strings.ToUpper(from), strings.ToUpper(to)
	if from == "" || to == "" {
		return "", fmt.Errorf("%w: exchange rate needs both currencies", ErrInvalidRequest)
	}
	if from == to {
		return "1", nil
	}
	rates, err := c.ExchangeRates(ctx, from)
	if err != nil {
		return "", err
	}
	rate, ok := rates[to]
	if !ok {
		return "", fmt.Errorf("no %s to %s exchange rate: %w", from, to, ErrNotFound)
	}
	return rate, nil
}
