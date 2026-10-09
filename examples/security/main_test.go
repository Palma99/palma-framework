package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/palma99/palma-framework/security"
)

func TestGeneratedApplicationAuthenticationAndAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name, session, document string
		status                  int
	}{
		{"missing credentials", "", "doc-1", 401},
		{"unknown session", "unknown", "doc-1", 401},
		{"expired session", "demo-expired", "doc-1", 401},
		{"revoked session", "demo-revoked", "doc-1", 401},
		{"deleted user", "demo-deleted", "doc-1", 401},
		{"disabled user", "demo-disabled", "doc-1", 403},
		{"owner", "demo-alice", "doc-1", 204},
		{"other owner", "demo-bob", "doc-2", 204},
		{"not owner", "demo-alice", "doc-2", 403},
		{"admin", "demo-admin", "doc-2", 204},
		{"admin other organization", "demo-admin", "doc-3", 403},
		{"missing document", "demo-alice", "missing", 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, err := Initialize()
			if err != nil {
				t.Fatal(err)
			}
			before, _ := app.Store.GetDocument(context.Background(), tc.document)
			r := httptest.NewRequest("PATCH", "/documents/"+tc.document, strings.NewReader(`{"title":"Updated"}`))
			r.Header.Set("Content-Type", "application/json")
			if tc.session != "" {
				r.AddCookie(&http.Cookie{Name: "session", Value: tc.session})
			}
			w := httptest.NewRecorder()
			app.Server.HTTPServer().Handler.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("response: %d %s", w.Code, w.Body)
			}
			after, _ := app.Store.GetDocument(context.Background(), tc.document)
			if tc.status == 204 {
				if after.Title != "Updated" {
					t.Fatal("authorized update did not use shared DI repository")
				}
			} else if before != after {
				t.Fatal("rejected request modified the document")
			}
		})
	}
}

func TestOptionalPrincipalAndCrossOriginProtection(t *testing.T) {
	app, err := Initialize()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		session, body string
		status        int
	}{
		{"", `"authenticated":false`, 200},
		{"demo-alice", `"user_id":"alice"`, 200},
		{"invalid", `"code":"unauthenticated"`, 401},
	} {
		r := httptest.NewRequest("GET", "/me", nil)
		if tc.session != "" {
			r.AddCookie(&http.Cookie{Name: "session", Value: tc.session})
		}
		w := httptest.NewRecorder()
		app.Server.HTTPServer().Handler.ServeHTTP(w, r)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.body) {
			t.Fatalf("me: %d %s", w.Code, w.Body)
		}
	}
	r := httptest.NewRequest("PATCH", "/documents/doc-1", strings.NewReader(`{"title":"Forged"}`))
	r.AddCookie(&http.Cookie{Name: "session", Value: "demo-alice"})
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	app.Server.HTTPServer().Handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("cross-origin: %d %s", w.Code, w.Body)
	}
	document, _ := app.Store.GetDocument(context.Background(), "doc-1")
	if document.Title == "Forged" {
		t.Fatal("cross-origin update was applied")
	}
}

func TestServiceEnforcesPolicyOutsideHTTP(t *testing.T) {
	app, err := Initialize()
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Service.UpdateTitle(context.Background(), "doc-1", "Anonymous"); !errors.Is(err, security.ErrUnauthenticated) {
		t.Fatalf("missing principal: %v", err)
	}
	ctx := security.WithPrincipal(context.Background(), AppPrincipal{UserID: "bob", OrganizationID: "one"})
	if err := app.Service.UpdateTitle(ctx, "doc-1", "Denied"); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("ownership: %v", err)
	}
	policy := NewDocumentAuthorizer()
	if err := policy.Check(ctx, AppPrincipal{OrganizationID: "one", Roles: []string{"admin"}}, "unknown", Document{OrganizationID: "one"}); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("unsupported action: %v", err)
	}
}
