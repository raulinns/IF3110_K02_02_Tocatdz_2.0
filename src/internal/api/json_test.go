package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type exampleInput struct {
	Title string `json:"title"`
	Count int    `json:"count"`
	Items []struct {
		Name string `json:"name"`
	} `json:"items"`
	Optional *bool          `json:"optional"`
	Data     map[string]any `json:"data"`
}

func TestDecodeJSONRejectsAmbiguousOrInvalidInputsWithoutPartialAssignment(t *testing.T) {
	for _, test := range []struct{ name, body string }{
		{"empty", ""}, {"whitespace", "  "}, {"null", "null"}, {"array", "[]"},
		{"unknown", `{"title":"new","password-canary":true}`},
		{"case", `{"Title":"new"}`}, {"duplicate", `{"title":"a","title":"b"}`},
		{"escaped duplicate", `{"title":"a","\u0074itle":"b"}`},
		{"nested duplicate", `{"items":[{"name":"a","name":"b"}]}`},
		{"nested unknown", `{"items":[{"name":"a","secret":true}]}`},
		{"map duplicate", `{"data":{"a":1,"a":2}}`},
		{"trailing document", `{"title":"a"} {"title":"b"}`},
		{"trailing junk", `{"title":"a"} garbage`},
		{"scalar type", `{"title":"new","count":"oops"}`},
		{"integer overflow", `{"count":99999999999999999999999999}`},
		{"fraction", `{"count":1.5}`}, {"null scalar", `{"count":null}`},
		{"syntax", `{"title":}`}, {"utf8", "{\"title\":\"\xff\"}"},
		{"deep", `{"data":` + strings.Repeat(`{"a":`, 70) + `1` + strings.Repeat(`}`, 70) + `}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", strings.NewReader(test.body))
			r.Header.Set("Content-Type", "application/json")
			input := exampleInput{Title: "unchanged", Count: 99}
			err := DecodeJSON(httptest.NewRecorder(), r, &input)
			if !errors.Is(err, security.ErrMalformedRequest) || input.Title != "unchanged" || input.Count != 99 {
				t.Fatalf("accepted invalid JSON or changed dst: %v, %+v", err, input)
			}
			if strings.Contains(err.Error(), "canary") {
				t.Fatal("error echoed user input")
			}
		})
	}
}

func TestDecodeJSONValidObjectsAndBodyLimits(t *testing.T) {
	for _, contentType := range []string{"application/json", "application/json; charset=UTF-8"} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(`{"title":"valid","count":2,"items":[{"name":"row"}],"optional":null,"data":{"a":[1,true,null]}}`))
		r.Header.Set("Content-Type", contentType)
		var input exampleInput
		if err := DecodeJSON(httptest.NewRecorder(), r, &input); err != nil || input.Title != "valid" || input.Items[0].Name != "row" {
			t.Fatalf("valid decode failed: %v", err)
		}
	}
	for _, chunked := range []bool{false, true} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(strings.Repeat(" ", int(MaxJSONBodyBytes))+`{}`))
		r.Header.Set("Content-Type", "application/json")
		if chunked {
			r.ContentLength = -1
		}
		var input exampleInput
		if err := DecodeJSON(httptest.NewRecorder(), r, &input); !errors.Is(err, security.ErrPayloadTooLarge) {
			t.Fatalf("body limit (chunked=%v): %v", chunked, err)
		}
	}
	// The login guard's smaller limit survives the generic decoder's outer limit.
	r := httptest.NewRequest("POST", "/", strings.NewReader(strings.Repeat(" ", int(security.MaxLoginFormBytes))+`{}`))
	r.Header.Set("Content-Type", "application/json")
	r.ContentLength = -1
	r.Body = http.MaxBytesReader(httptest.NewRecorder(), r.Body, security.MaxLoginFormBytes)
	var input exampleInput
	if err := DecodeJSON(httptest.NewRecorder(), r, &input); !errors.Is(err, security.ErrPayloadTooLarge) {
		t.Fatalf("login body limit lost: %v", err)
	}
}

func TestDecodeJSONMediaTypesAndDestination(t *testing.T) {
	for _, ct := range []string{"", "text/plain", "application/x-www-form-urlencoded", "application/problem+json", "application/json; charset=latin1", "application/json; boundary=x"} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(`{}`))
		r.Header.Set("Content-Type", ct)
		var input exampleInput
		if err := DecodeJSON(httptest.NewRecorder(), r, &input); !errors.Is(err, security.ErrUnsupportedMediaType) {
			t.Errorf("media type %q: %v", ct, err)
		}
	}
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{}`))
	r.Header.Add("Content-Type", "application/json")
	r.Header.Add("Content-Type", "application/json")
	var input exampleInput
	if err := DecodeJSON(httptest.NewRecorder(), r, &input); !errors.Is(err, security.ErrUnsupportedMediaType) {
		t.Fatal("duplicate content type accepted")
	}
	for _, dst := range []any{nil, (*exampleInput)(nil), exampleInput{}, new([]string)} {
		if err := DecodeJSON(httptest.NewRecorder(), r, dst); !errors.Is(err, errJSONDestination) {
			t.Fatal("invalid DTO destination accepted")
		}
	}
}

func TestWriteJSONDoesNotCommitAnUnencodableResponse(t *testing.T) {
	w := httptest.NewRecorder()
	if err := WriteJSON(w, 200, map[string]any{"secret": security.NewSecret("canary")}); err == nil {
		t.Fatal("secret serialized")
	}
	if w.Body.Len() != 0 || w.Header().Get("Content-Type") != "" {
		t.Fatal("partial response committed before encoding succeeded")
	}
}
