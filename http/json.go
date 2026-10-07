package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	stdhttp "net/http"
)

type DecodeOptions struct {
	// MaxBodyBytes defaults to 1 MiB. The body is read with a bounded allocation.
	MaxBodyBytes int64
	// Unknown fields are rejected by default; opt in to compatibility if needed.
	AllowUnknownFields bool
}

// DecodeJSON reads one JSON value, enforcing content type and body size.
// Request errors are *Error values with a safe public message and internal cause.
func DecodeJSON[T any](r *stdhttp.Request, options DecodeOptions) (T, error) {
	var value T
	if r == nil {
		return value, fmt.Errorf("pfwhttp: request must not be nil")
	}
	limit := options.MaxBodyBytes
	if limit == 0 {
		limit = 1 << 20
	}
	if limit < 0 || limit == math.MaxInt64 {
		return value, fmt.Errorf("pfwhttp: invalid body limit")
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return value, NewError(415, "unsupported_media_type", "Content-Type must be application/json").WithCause(err)
	}
	invalid := func(cause error) (T, error) {
		var zero T
		return zero, NewError(400, "invalid_request", "invalid JSON body").WithCause(cause)
	}
	if r.Body == nil {
		return invalid(io.EOF)
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return invalid(err)
	}
	if int64(len(data)) > limit {
		return value, NewError(413, "payload_too_large", "request body is too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if !options.AllowUnknownFields {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(&value); err != nil {
		return invalid(err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return invalid(err)
	}
	return value, nil
}
