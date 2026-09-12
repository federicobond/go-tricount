package tricount

import (
	"encoding/json"
	"fmt"
	"time"
)

// Every API response is shaped {"Response": [{"SomeKey": {...}}, ...]}.
type envelope struct {
	Response []json.RawMessage `json:"Response"`
}

// decodeEnvelope unwraps each {"key": {...}} element of the response array
// into a T, skipping elements that carry a different key.
func decodeEnvelope[T any](body []byte, key string) ([]T, error) {
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("tricount: decoding response envelope: %w", err)
	}
	var out []T
	for _, raw := range env.Response {
		var byKey map[string]json.RawMessage
		if err := json.Unmarshal(raw, &byKey); err != nil {
			return nil, fmt.Errorf("tricount: decoding response element: %w", err)
		}
		inner, ok := byKey[key]
		if !ok {
			continue
		}
		var v T
		if err := json.Unmarshal(inner, &v); err != nil {
			return nil, fmt.Errorf("tricount: decoding %s: %w", key, err)
		}
		out = append(out, v)
	}
	return out, nil
}

// decodeID reads the {"Response":[{"Id":{"id":N}}]} reply that mutating
// endpoints return.
func decodeID(body []byte) (int64, error) {
	type idObject struct {
		ID int64 `json:"id"`
	}
	ids, err := decodeEnvelope[idObject](body, "Id")
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, fmt.Errorf("tricount: response carried no Id: %s", truncate(body))
	}
	return ids[0].ID, nil
}

// firstValue returns the sole value of a single-key wrapper object. The API
// wraps list elements this way, for example
// {"RegistryMembershipNonUser": {...}}.
func firstValue[T any](m map[string]T) (T, bool) {
	for _, v := range m {
		return v, true
	}
	var zero T
	return zero, false
}

func truncate(b []byte) string {
	if len(b) > 256 {
		return string(b[:256])
	}
	return string(b)
}

// apiTimeLayout is the API's timestamp format: no timezone, six fractional
// digits, a space rather than a T.
const apiTimeLayout = "2006-01-02 15:04:05.000000"

const apiTimeLayoutNoFraction = "2006-01-02 15:04:05"

// apiTime adapts time.Time to the API's timestamp format. It exists only in
// the wire layer; the domain types use time.Time.
type apiTime struct {
	time.Time
}

func (t apiTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.Format(apiTimeLayout))
}

func (t *apiTime) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		t.Time = time.Time{}
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	if s == "" {
		t.Time = time.Time{}
		return nil
	}
	for _, layout := range []string{apiTimeLayout, apiTimeLayoutNoFraction} {
		if v, err := time.Parse(layout, s); err == nil {
			t.Time = v
			return nil
		}
	}
	return fmt.Errorf("tricount: %q is not a recognised timestamp", s)
}

// decodeUUID reads the {"Response":[{"UUID":{"uuid":"..."}}]} reply that
// gallery uploads return.
func decodeUUID(body []byte) (string, error) {
	type uuidObject struct {
		UUID string `json:"uuid"`
	}
	uuids, err := decodeEnvelope[uuidObject](body, "UUID")
	if err != nil {
		return "", err
	}
	if len(uuids) == 0 || uuids[0].UUID == "" {
		return "", fmt.Errorf("tricount: response carried no UUID: %s", truncate(body))
	}
	return uuids[0].UUID, nil
}
