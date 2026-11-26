package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const (
	MaxJSONBodyBytes int64 = 2 << 20
	maxJSONDepth           = 64
)

var (
	errJSONDestination = errors.New("JSON destination must be a non-nil DTO struct pointer")
	errJSONResponse    = errors.New("response DTO cannot be JSON encoded")
)

// DecodeJSON accepts exactly one UTF-8 object into a pointer to an explicit DTO.
// It never partially modifies dst on failure. Required/semantic field validation
// belongs to the domain; use pointer fields where absent differs from false/zero.
// The json_required:"true" tag checks key presence, including for nullable fields.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	value := reflect.ValueOf(dst)
	if value.Kind() != reflect.Pointer || value.IsNil() || value.Elem().Kind() != reflect.Struct {
		return errJSONDestination // Programmer/configuration error, not bad client input.
	}
	if !security.JSONMediaType(r) {
		return security.ErrUnsupportedMediaType
	}
	if r.ContentLength > MaxJSONBodyBytes {
		return security.ErrPayloadTooLarge
	}
	if r.Body == nil {
		return security.ErrMalformedRequest
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxJSONBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			return security.ErrPayloadTooLarge
		}
		return security.ErrMalformedRequest
	}
	if !utf8.Valid(body) || len(bytes.TrimSpace(body)) == 0 || bytes.TrimSpace(body)[0] != '{' {
		return security.ErrMalformedRequest
	}
	// DisallowUnknownFields alone accepts duplicate keys and case-insensitive
	// matches. The token pass rejects both (including escaped duplicate names).
	check := json.NewDecoder(bytes.NewReader(body))
	check.UseNumber()
	if err := inspectValue(check, value.Elem().Type(), 0); err != nil {
		return security.ErrMalformedRequest
	}
	if _, err := check.Token(); err != io.EOF {
		return security.ErrMalformedRequest
	}
	candidate := reflect.New(value.Elem().Type())
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(candidate.Interface()); err != nil {
		return security.ErrMalformedRequest
	}
	value.Elem().Set(candidate.Elem())
	return nil
}

// inspectValue validates key identity/shape against the DTO without retaining
// user values. Nesting is bounded independently of the body-size bound.
func inspectValue(decoder *json.Decoder, typ reflect.Type, depth int) error {
	if depth > maxJSONDepth {
		return security.ErrMalformedRequest
	}
	nullable := typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Map || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Interface
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token == nil {
		if nullable {
			return nil
		}
		return security.ErrMalformedRequest
	}
	switch token {
	case json.Delim('{'):
		if typ.Kind() != reflect.Struct && typ.Kind() != reflect.Map && typ.Kind() != reflect.Interface {
			return security.ErrMalformedRequest
		}
		seen := map[string]bool{}
		fields := jsonFields(typ)
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok || seen[key] {
				return security.ErrMalformedRequest
			}
			seen[key] = true
			var child reflect.Type
			switch typ.Kind() {
			case reflect.Struct:
				child = fields[key].typ
				if child == nil {
					return security.ErrMalformedRequest
				}
			case reflect.Map:
				child = typ.Elem()
			default:
				child = typ
			}
			if err := inspectValue(decoder, child, depth+1); err != nil {
				return err
			}
		}
		for name, field := range fields {
			if field.required && !seen[name] {
				return security.ErrMalformedRequest
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return security.ErrMalformedRequest
		}
	case json.Delim('['):
		child := typ
		if typ.Kind() == reflect.Array || typ.Kind() == reflect.Slice {
			child = typ.Elem()
		} else if typ.Kind() != reflect.Interface {
			return security.ErrMalformedRequest
		}
		for decoder.More() {
			if err := inspectValue(decoder, child, depth+1); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return security.ErrMalformedRequest
		}
	case json.Delim('}'), json.Delim(']'):
		return security.ErrMalformedRequest
	}
	return nil // json.Decoder performs scalar type/range validation in the second pass.
}

type jsonField struct {
	typ      reflect.Type
	required bool
}

func jsonFields(typ reflect.Type) map[string]jsonField {
	fields := map[string]jsonField{}
	if typ.Kind() != reflect.Struct {
		return fields
	}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		if field.Anonymous && name == "" {
			child := field.Type
			for child.Kind() == reflect.Pointer {
				child = child.Elem()
			}
			for name, typ := range jsonFields(child) {
				fields[name] = typ
			}
			continue
		}
		if !field.IsExported() {
			continue
		}
		if name == "" {
			name = field.Name
		}
		fields[name] = jsonField{typ: field.Type, required: field.Tag.Get("json_required") == "true"}
	}
	return fields
}

// RequireNoBody validates body-less operations before they perform side effects.
func RequireNoBody(r *http.Request) error {
	if r.Body == nil {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1))
	if err != nil || len(body) != 0 {
		return security.ErrMalformedRequest
	}
	return nil
}

// WriteJSON encodes before committing headers. An unsupported value/Secret cannot
// leave a successful status or half a JSON object containing other sensitive data.
func WriteJSON(w http.ResponseWriter, status int, value any) error {
	return writeJSON(w, status, value, 0)
}

func writeJSON(w http.ResponseWriter, status int, value any, limit int) error {
	if status < 200 || status > 599 || status == http.StatusNoContent || status == http.StatusNotModified {
		return security.ErrUnavailable
	}
	body, err := json.Marshal(value)
	if err != nil {
		return errJSONResponse
	}
	if limit > 0 && len(body)+1 > limit {
		return security.ErrResultTooLarge
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, err = w.Write(append(body, '\n'))
	return err
}
