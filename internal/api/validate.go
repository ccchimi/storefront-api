package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
)

// decodeObject reads the request body as a JSON object.
//
// Decoding into map[string]any with UseNumber (instead of a typed struct)
// is deliberate: it lets validation tell apart 2, "2", 2.5, true, null and a
// missing field, which a struct with int fields would silently blur.
func decodeObject(r *http.Request) (map[string]any, bool) {
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()

	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, false
	}
	// Exactly one value: "{} {}" or "{}garbage" is not a valid body.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, false
	}
	obj, ok := v.(map[string]any)
	return obj, ok
}

func invalidBody() map[string]string {
	return map[string]string{"body": "must be a valid JSON object"}
}

// intField returns the value of key if it is a JSON integer. Strings,
// decimals (including 1.0 and 1e2), booleans, null and missing keys all fail.
func intField(obj map[string]any, key string) (int64, bool) {
	n, ok := obj[key].(json.Number)
	if !ok {
		return 0, false
	}
	i, err := strconv.ParseInt(n.String(), 10, 64)
	return i, err == nil
}

// stringField returns the value of key if it is a JSON string.
func stringField(obj map[string]any, key string) (string, bool) {
	s, ok := obj[key].(string)
	return s, ok
}

// validEmail accepts a bare address like "a@b.co". net/mail on its own is
// too lenient for this (it accepts "Name <a@b>" and dotless domains).
func validEmail(s string) bool {
	if len(s) > 254 {
		return false
	}
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Address != s {
		return false
	}
	_, domain, _ := strings.Cut(s, "@")
	return strings.Contains(domain, ".") && !strings.HasPrefix(domain, ".") && !strings.HasSuffix(domain, ".")
}

// positiveQueryInt parses an optional query parameter. present is false when
// the parameter wasn't sent at all.
func positiveQueryInt(r *http.Request, name string) (value int, present, ok bool) {
	q := r.URL.Query()
	if !q.Has(name) {
		return 0, false, true
	}
	n, err := strconv.Atoi(q.Get(name))
	if err != nil || n < 1 {
		return 0, true, false
	}
	return n, true, true
}
