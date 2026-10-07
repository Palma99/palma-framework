package http_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	pfwhttp "github.com/palma99/palma-framework/http"
)

func TestResponses(t *testing.T) {
	w := httptest.NewRecorder()
	if err := pfwhttp.Created("/users/2", map[string]string{"name": "Ada"}).Write(w); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusCreated || w.Header().Get("Location") != "/users/2" || w.Header().Get("Content-Type") != "application/json" || w.Body.String() != "{\"name\":\"Ada\"}\n" {
		t.Fatalf("response: %d %v %s", w.Code, w.Header(), w.Body)
	}
	w = httptest.NewRecorder()
	if err := pfwhttp.NoContent().Write(w); err != nil || w.Code != 204 || w.Body.Len() != 0 || w.Header().Get("Content-Type") != "" {
		t.Fatalf("no content: %d %v %v", w.Code, w.Header(), err)
	}
	w = httptest.NewRecorder()
	w.Header().Set("X-Before", "kept")
	response := pfwhttp.JSON(200, make(chan int))
	response.Headers = http.Header{"X-After": {"should not appear"}}
	if err := response.Write(w); err == nil || w.Body.Len() != 0 || w.Header().Get("X-After") != "" {
		t.Fatalf("encoding failure committed response: %v %s", err, w.Body)
	}
}

type input struct {
	Name string `json:"name"`
}

func TestDecodeJSON(t *testing.T) {
	for _, tc := range []struct {
		name, body, media string
		opts              pfwhttp.DecodeOptions
		status            int
	}{
		{"success", `{"name":"Ada"}`, "application/json; charset=utf-8", pfwhttp.DecodeOptions{}, 0},
		{"unknown allowed", `{"name":"Ada","extra":true}`, "application/json", pfwhttp.DecodeOptions{AllowUnknownFields: true}, 0},
		{"malformed", `{"name":`, "application/json", pfwhttp.DecodeOptions{}, 400},
		{"wrong type", `{"name":42}`, "application/json", pfwhttp.DecodeOptions{}, 400},
		{"unknown", `{"extra":true}`, "application/json", pfwhttp.DecodeOptions{}, 400},
		{"trailing JSON", `{"name":"Ada"} {}`, "application/json", pfwhttp.DecodeOptions{}, 400},
		{"trailing garbage", `{"name":"Ada"} trailing`, "application/json", pfwhttp.DecodeOptions{}, 400},
		{"empty", "", "application/json", pfwhttp.DecodeOptions{}, 400},
		{"media type", `{"name":"Ada"}`, "text/plain", pfwhttp.DecodeOptions{}, 415},
		{"too large", `{"name":"Ada"}`, "application/json", pfwhttp.DecodeOptions{MaxBodyBytes: 8}, 413},
		{"exact limit", `{"name":"Ada"}`, "application/json", pfwhttp.DecodeOptions{MaxBodyBytes: 14}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", tc.media)
			value, err := pfwhttp.DecodeJSON[input](r, tc.opts)
			if tc.status == 0 {
				if err != nil || value.Name != "Ada" {
					t.Fatalf("decode: %+v %v", value, err)
				}
				return
			}
			var failure *pfwhttp.Error
			if !errors.As(err, &failure) || failure.Status != tc.status || value != (input{}) {
				t.Fatalf("decode error: %+v %v", value, err)
			}
		})
	}
}

type validationError struct{ Field string }

func (e *validationError) Error() string { return "private domain detail" }

func TestMapperPreservesCausesAndPublicPayload(t *testing.T) {
	missing := errors.New("private storage error")
	mapper := pfwhttp.NewMapper(
		pfwhttp.As(func(invalid *validationError) *pfwhttp.Error {
			return pfwhttp.NewError(422, "validation_failed", "invalid input").WithFields(map[string]string{invalid.Field: "required"})
		}),
		pfwhttp.Is(missing, pfwhttp.NewError(404, "not_found", "user not found")),
	)
	wrapped := fmt.Errorf("repository: %w", missing)
	mapped := mapper.Map(wrapped)
	if mapped.Status != 404 || !errors.Is(mapped, missing) || !errors.Is(mapped, wrapped) {
		t.Fatalf("mapping: %+v", mapped)
	}
	invalid := &validationError{Field: "name"}
	mapped = mapper.Map(fmt.Errorf("application: %w", invalid))
	var cause *validationError
	if mapped.Status != 422 || !errors.As(mapped, &cause) || cause != invalid || mapped.Fields["name"] != "required" {
		t.Fatalf("typed mapping: %+v", mapped)
	}
	unknown := errors.New("database password=secret")
	w := httptest.NewRecorder()
	if err := mapper.Write(w, unknown); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if w.Code != 500 || payload["code"] != "internal_error" || strings.Contains(w.Body.String(), "secret") || payload["Status"] != nil || payload["cause"] != nil {
		t.Fatalf("public payload: %s", w.Body)
	}
	direct := pfwhttp.NewError(400, "invalid_request", "invalid JSON body").WithCause(unknown)
	wrapped = fmt.Errorf("transport: %w", direct)
	mapped = mapper.Map(wrapped)
	if mapped.Status != 400 || !errors.Is(mapped, direct) || !errors.Is(mapped, unknown) {
		t.Fatalf("direct HTTP error: %+v", mapped)
	}
	if mapper.Map(nil) != nil {
		t.Fatal("nil error must remain nil")
	}
}

func TestMapperConcurrentUseAndRuleOrder(t *testing.T) {
	cause := errors.New("match")
	fields := map[string]string{"name": "required"}
	first := pfwhttp.NewError(422, "first", "first rule").WithFields(fields)
	fields["name"] = "changed externally"
	mapper := pfwhttp.NewMapper(pfwhttp.Is(cause, first), pfwhttp.Is(cause, pfwhttp.NewError(400, "second", "second rule")))
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result := mapper.Map(cause)
			if result.Code != "first" || result.Fields["name"] != "required" {
				t.Errorf("mapping: %+v", result)
			}
			result.Fields["name"] = "changed by request"
		}()
	}
	wg.Wait()
	if first.Fields["name"] != "required" {
		t.Fatal("shared rule was mutated")
	}
}
