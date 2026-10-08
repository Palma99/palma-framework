// Package config loads environment variables into typed application configuration.
// It uses reflection for configuration decoding, independently of build-time DI.
package config

import (
	"encoding"
	"fmt"
	pfw "github.com/palma99/palma-framework"
	"math"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type Options struct {
	Prefix string
	// Lookup defaults to os.LookupEnv. Supply an isolated lookup in tests.
	Lookup func(string) (string, bool)
	// EnvFiles are read in order; later files override earlier ones. Lookup wins.
	EnvFiles []string
	// IgnoreMissingEnvFiles ignores absent files, never syntax/read errors.
	IgnoreMissingEnvFiles bool
	// Environment selects .env, .env.<name>, .env.<name>.local when EnvFiles is empty.
	Environment pfw.Environment
	EnvDir      string
}

// Validator is optionally implemented by the configuration value or its pointer.
// Validation messages are supplied by the application and should omit secrets.
type Validator interface{ Validate() error }

type EnvironmentValidator interface{ ValidateEnvironment(pfw.Environment) error }

type FieldError struct{ Field, Variable, Reason string }

func (e *FieldError) Error() string {
	if e.Variable == "" {
		return e.Field + ": " + e.Reason
	}
	return e.Field + " (" + e.Variable + "): " + e.Reason
}

type Error struct{ Fields []*FieldError }

func (e *Error) Error() string {
	var lines []string
	for _, field := range e.Fields {
		lines = append(lines, field.Error())
	}
	return "invalid configuration:\n" + strings.Join(lines, "\n")
}
func (e *Error) Unwrap() []error {
	var result []error
	for _, field := range e.Fields {
		result = append(result, field)
	}
	return result
}

// Invalid creates a validation error without requiring an environment key.
func Invalid(field, reason string) error { return &FieldError{Field: field, Reason: reason} }

// Load returns a fully parsed and validated struct, or its zero value on error.
// Defaults apply only to missing keys: present empty values never use defaults.
func Load[T any](options Options) (T, error) {
	var value, zero T
	v := reflect.ValueOf(&value).Elem()
	if v.Kind() != reflect.Struct {
		return zero, fmt.Errorf("config: Load[T] requires a struct type")
	}
	lookup := options.Lookup
	if lookup == nil {
		lookup = os.LookupEnv
	}
	lookup, err := fileLookup(options, lookup)
	if err != nil {
		return zero, err
	}
	l := loader{lookup: lookup, keys: make(map[string]string), active: make(map[reflect.Type]bool)}
	l.walk(v, "", options.Prefix)
	if len(l.problems) > 0 {
		return zero, &Error{Fields: l.problems}
	}
	if validator, ok := any(&value).(Validator); ok {
		if err := validator.Validate(); err != nil {
			return zero, err
		}
	} else if validator, ok := any(value).(Validator); ok {
		if err := validator.Validate(); err != nil {
			return zero, err
		}
	}
	if options.Environment != "" {
		if validator, ok := any(&value).(EnvironmentValidator); ok {
			if err := validator.ValidateEnvironment(options.Environment); err != nil {
				return zero, err
			}
		}
	}
	return value, nil
}

type loader struct {
	lookup   func(string) (string, bool)
	keys     map[string]string
	active   map[reflect.Type]bool
	problems []*FieldError
}

func (l *loader) fail(field, key, reason string) {
	l.problems = append(l.problems, &FieldError{Field: field, Variable: key, Reason: reason})
}

// walk reports whether any nested value was supplied, for optional struct pointers.
func (l *loader) walk(value reflect.Value, path, prefix string) bool {
	if l.active[value.Type()] {
		l.fail(path, "", "recursive configuration is unsupported")
		return false
	}
	l.active[value.Type()] = true
	defer delete(l.active, value.Type())
	touched := false
	for n := 0; n < value.NumField(); n++ {
		field := value.Type().Field(n)
		v := value.Field(n)
		name := field.Name
		if path != "" {
			name = path + "." + name
		}
		key, tagged := field.Tag.Lookup("env")
		def, hasDefault := field.Tag.Lookup("envDefault")
		requiredTag, hasRequired := field.Tag.Lookup("envRequired")
		nestedPrefix, hasPrefix := field.Tag.Lookup("envPrefix")
		separator, hasSeparator := field.Tag.Lookup("envSeparator")
		if key == "-" {
			continue
		}
		if field.PkgPath != "" {
			if tagged || hasDefault || hasRequired || hasPrefix || hasSeparator {
				l.fail(name, "", "environment tags require an exported field")
			}
			continue
		}
		if !tagged {
			nested := v
			if v.Kind() == reflect.Pointer && v.Type().Elem().Kind() == reflect.Struct {
				nested = reflect.New(v.Type().Elem()).Elem()
			}
			if nested.Kind() == reflect.Struct {
				if hasDefault || hasRequired || hasSeparator {
					l.fail(name, "", "nested configuration supports only envPrefix; tag scalar fields with env")
					continue
				}
				supplied := l.walk(nested, name, prefix+nestedPrefix)
				if v.Kind() == reflect.Pointer && supplied {
					ptr := reflect.New(nested.Type())
					ptr.Elem().Set(nested)
					v.Set(ptr)
				}
				touched = touched || supplied
			} else if hasDefault || hasRequired || hasPrefix || hasSeparator {
				l.fail(name, "", "env tag is required")
			}
			continue
		}
		key = prefix + key
		if key == prefix || strings.ContainsAny(key, "=\x00") {
			l.fail(name, key, "invalid environment key")
			continue
		}
		if previous, exists := l.keys[key]; exists {
			l.fail(name, key, "key already mapped to "+previous)
			continue
		}
		l.keys[key] = name
		if hasPrefix {
			l.fail(name, key, "envPrefix is valid only on nested structs")
			continue
		}
		required := false
		if hasRequired {
			if requiredTag != "true" && requiredTag != "false" {
				l.fail(name, key, "envRequired must be true or false")
				continue
			}
			required = requiredTag == "true"
		}
		if required && hasDefault {
			l.fail(name, key, "envRequired and envDefault cannot be combined")
			continue
		}
		if hasSeparator && (v.Kind() != reflect.Slice || separator == "") {
			l.fail(name, key, "envSeparator requires a slice and a non-empty delimiter")
			continue
		}
		if separator == "" {
			separator = ","
		}
		if !supported(v.Type()) {
			l.fail(name, key, "unsupported field type "+v.Kind().String())
			continue
		}
		raw, present := l.lookup(key)
		if !present && hasDefault {
			raw, present = def, true
		}
		if !present || (required && raw == "") {
			if required {
				l.fail(name, key, "required environment variable is missing or empty")
			}
			continue
		}
		touched = true
		if reason := decode(v, raw, separator); reason != "" {
			l.fail(name, key, reason)
		}
	}
	return touched
}

var durationType = reflect.TypeFor[time.Duration]()
var textType = reflect.TypeFor[encoding.TextUnmarshaler]()

func supported(t reflect.Type) bool { return supports(t, make(map[reflect.Type]bool)) }
func supports(t reflect.Type, seen map[reflect.Type]bool) bool {
	if seen[t] {
		return false
	}
	seen[t] = true
	defer delete(seen, t)
	if t.Implements(textType) || reflect.PointerTo(t).Implements(textType) {
		return true
	}
	if t.Kind() == reflect.Pointer {
		return supports(t.Elem(), seen)
	}
	if t.Kind() == reflect.Slice {
		return t.Elem().Kind() != reflect.Slice && t.Elem().Kind() != reflect.Pointer && supports(t.Elem(), seen)
	}
	switch t.Kind() {
	case reflect.String, reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

func decode(v reflect.Value, raw, separator string) string {
	if v.Kind() == reflect.Pointer {
		v.Set(reflect.New(v.Type().Elem()))
		return decode(v.Elem(), raw, separator)
	}
	if v.CanAddr() && v.Addr().Type().Implements(textType) {
		if err := v.Addr().Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(raw)); err != nil {
			return "invalid " + v.Type().String()
		}
		return ""
	}
	if v.Type() == durationType {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return "expected duration with units"
		}
		v.SetInt(int64(d))
		return ""
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString(raw)
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return "expected boolean"
		}
		v.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, v.Type().Bits())
		if err != nil {
			return "expected " + v.Type().String() + " integer"
		}
		v.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(raw, 10, v.Type().Bits())
		if err != nil {
			return "expected " + v.Type().String() + " unsigned integer"
		}
		v.SetUint(n)
	case reflect.Float32, reflect.Float64:
		n, err := strconv.ParseFloat(raw, v.Type().Bits())
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return "expected finite " + v.Type().String() + " number"
		}
		v.SetFloat(n)
	case reflect.Slice:
		parts := []string{}
		if raw != "" {
			parts = strings.Split(raw, separator)
		}
		result := reflect.MakeSlice(v.Type(), len(parts), len(parts))
		for n, part := range parts {
			if reason := decode(result.Index(n), strings.TrimSpace(part), separator); reason != "" {
				return fmt.Sprintf("list element %d: %s", n+1, reason)
			}
		}
		v.Set(result)
	default:
		return "unsupported field type"
	}
	return ""
}
