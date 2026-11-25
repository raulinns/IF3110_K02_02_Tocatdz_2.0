package api

import (
	"net/http"
	"net/url"
	"slices"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type pageInfo struct {
	Page    int  `json:"page"`
	Limit   int  `json:"limit"`
	HasNext bool `json:"has_next"`
}

// ParseQuery rejects malformed escaping, unknown names and repeated keys. Values
// remain strings for endpoint-specific semantic validation; no request data is
// included in errors. An empty allowlist rejects every query parameter.
func ParseQuery(r *http.Request, allowed ...string) (url.Values, error) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, security.ErrMalformedRequest
	}
	for key, values := range query {
		if !slices.Contains(allowed, key) || len(values) != 1 {
			return nil, security.ErrMalformedRequest
		}
	}
	return query, nil
}

func PathID(r *http.Request, name string) (security.ID, error) {
	id := security.ID(r.PathValue(name))
	if !security.ValidID(id) {
		return "", security.ErrMalformedRequest
	}
	return id, nil
}
