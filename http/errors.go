package http

import (
	"errors"
	stdhttp "net/http"
)

// Error describes a public HTTP error. Its status and internal cause are not
// serialized, and domain errors need not import this package.
type Error struct {
	Status  int               `json:"-"`
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
	cause   error
}

func NewError(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}
func (e *Error) Error() string                { return e.Message }
func (e *Error) Unwrap() error                { return e.cause }
func (e *Error) WithCause(cause error) *Error { copy := *e; copy.cause = cause; return &copy }
func (e *Error) WithFields(fields map[string]string) *Error {
	copy := *e
	copy.Fields = make(map[string]string, len(fields))
	for key, value := range fields {
		copy.Fields[key] = value
	}
	return &copy
}
func (e *Error) Write(w stdhttp.ResponseWriter) error { return JSON(e.Status, e).Write(w) }

// Rule returns nil when it does not match. Rules must be safe for concurrent use.
type Rule func(error) *Error

// ErrorMapper is the contract used by HTTP consumers. Implementations map
// application errors and write their public response without exposing causes.
type ErrorMapper interface {
	Map(error) *Error
	Write(stdhttp.ResponseWriter, error) error
}

type Mapper struct{ rules []Rule }

var _ ErrorMapper = (*Mapper)(nil)

// NewMapper creates a mapper with ordered rules and a safe generic 500 fallback.
func NewMapper(rules ...Rule) *Mapper { return &Mapper{rules: append([]Rule(nil), rules...)} }

func Is(target error, response *Error) Rule {
	return func(err error) *Error {
		if target != nil && errors.Is(err, target) {
			return response
		}
		return nil
	}
}

func As[T error](mapError func(T) *Error) Rule {
	return func(err error) *Error {
		var typed T
		if errors.As(err, &typed) {
			return mapError(typed)
		}
		return nil
	}
}

// Map preserves already typed HTTP errors, then evaluates application rules.
// Unknown errors receive a generic public message while retaining the cause.
func (m *Mapper) Map(err error) *Error {
	if err == nil {
		return nil
	}
	var direct *Error
	if errors.As(err, &direct) && direct != nil {
		return direct.WithFields(direct.Fields).WithCause(err)
	}
	for _, rule := range m.rules {
		if rule != nil {
			if mapped := rule(err); mapped != nil {
				return mapped.WithFields(mapped.Fields).WithCause(err)
			}
		}
	}
	return NewError(stdhttp.StatusInternalServerError, "internal_error", "unable to process request").WithCause(err)
}

func (m *Mapper) Write(w stdhttp.ResponseWriter, err error) error {
	if mapped := m.Map(err); mapped != nil {
		return mapped.Write(w)
	}
	return nil
}
