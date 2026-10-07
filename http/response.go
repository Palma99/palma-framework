// Package http provides transport-independent HTTP responses, JSON decoding and
// application error mapping. Import it as pfwhttp alongside net/http.
package http

import (
	"encoding/json"
	"fmt"
	stdhttp "net/http"
)

type Response[T any] struct {
	Status  int
	Headers stdhttp.Header
	Body    T
}

func JSON[T any](status int, body T) Response[T] { return Response[T]{Status: status, Body: body} }
func OK[T any](body T) Response[T]               { return JSON(stdhttp.StatusOK, body) }
func Created[T any](location string, body T) Response[T] {
	return Response[T]{
		Status:  stdhttp.StatusCreated,
		Headers: stdhttp.Header{"Location": {location}},
		Body:    body}
}
func NoContent() Response[struct{}] { return JSON(stdhttp.StatusNoContent, struct{}{}) }

// Write serializes before committing headers, so encoding failures do not leave
// a partial successful response. Network write errors are returned to the caller.
func (r Response[T]) Write(w stdhttp.ResponseWriter) error {
	if r.Status < 200 || r.Status > 599 {
		return fmt.Errorf("pfwhttp: invalid response status %d", r.Status)
	}
	var data []byte
	if r.Status != stdhttp.StatusNoContent && r.Status != stdhttp.StatusNotModified {
		var err error
		data, err = json.Marshal(r.Body)
		if err != nil {
			return fmt.Errorf("pfwhttp: encode response: %w", err)
		}
		data = append(data, '\n')
	}
	for key, values := range r.Headers {
		w.Header()[stdhttp.CanonicalHeaderKey(key)] = append([]string(nil), values...)
	}
	if data != nil && w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(r.Status)
	if data != nil {
		_, err := w.Write(data)
		return err
	}
	return nil
}
