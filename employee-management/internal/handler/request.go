package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// maxBodyBytes caps request bodies so a client cannot exhaust memory.
const maxBodyBytes = 1 << 20 // 1 MiB

// requestError is a problem with the HTTP request itself (bad JSON, bad ID,
// bad query string), found before any business logic runs.
type requestError struct {
	status  int
	code    string
	message string
}

func (e *requestError) Error() string { return e.message }

func badRequest(code, format string, args ...any) error {
	return &requestError{status: http.StatusBadRequest, code: code, message: fmt.Sprintf(format, args...)}
}

// decodeJSON decodes a body holding exactly one JSON object into dst.
// Unknown fields are rejected, so a typo such as "salaryy" is reported
// instead of being silently ignored.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		var syntaxErr *json.SyntaxError
		var typeErr *json.UnmarshalTypeError
		var tooLarge *http.MaxBytesError
		switch {
		case errors.Is(err, io.EOF):
			return badRequest("INVALID_JSON", "request body must not be empty")
		case errors.Is(err, io.ErrUnexpectedEOF):
			return badRequest("INVALID_JSON", "request body contains malformed JSON")
		case errors.As(err, &syntaxErr):
			return badRequest("INVALID_JSON", "malformed JSON at byte %d", syntaxErr.Offset)
		case errors.As(err, &typeErr):
			if typeErr.Field == "" {
				return badRequest("INVALID_JSON", "request body must be a JSON object")
			}
			return badRequest("INVALID_JSON", "field %q must be a %s", typeErr.Field, typeErr.Type)
		case errors.As(err, &tooLarge):
			return &requestError{
				status:  http.StatusRequestEntityTooLarge,
				code:    "BODY_TOO_LARGE",
				message: fmt.Sprintf("request body must not exceed %d bytes", tooLarge.Limit),
			}
		default:
			// encoding/json has no typed error for unknown fields.
			if field, ok := strings.CutPrefix(err.Error(), "json: unknown field "); ok {
				return badRequest("INVALID_JSON", "unknown field %s", field)
			}
			return badRequest("INVALID_JSON", "invalid request body")
		}
	}

	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return badRequest("INVALID_JSON", "request body must contain a single JSON object")
	}
	return nil
}

// parseID reads the {id} path segment as a positive integer.
func parseID(r *http.Request) (int64, error) {
	raw := r.PathValue("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, badRequest("INVALID_ID", "id must be a positive integer, got %q", raw)
	}
	return id, nil
}

// parsePagination reads ?limit= and ?offset=. Missing values are 0; the
// service then applies the default and the maximum page size.
func parsePagination(q url.Values) (limit, offset int, err error) {
	if limit, err = queryInt(q, "limit"); err != nil {
		return 0, 0, err
	}
	if offset, err = queryInt(q, "offset"); err != nil {
		return 0, 0, err
	}
	return limit, offset, nil
}

func queryInt(q url.Values, key string) (int, error) {
	raw := q.Get(key)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, badRequest("INVALID_QUERY", "%s must be a non-negative integer, got %q", key, raw)
	}
	return n, nil
}
