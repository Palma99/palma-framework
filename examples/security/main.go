// This example uses seeded, in-memory sessions, not a login/session issuer.
package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"

	pfwhttp "github.com/palma99/palma-framework/http"
	"github.com/palma99/palma-framework/lifecycle"
	"github.com/palma99/palma-framework/security"
	securityhttp "github.com/palma99/palma-framework/security/http"
	"github.com/palma99/palma-framework/transport/httpserver"
)

//go:generate go run github.com/palma99/palma-framework/cmd/pfw generate

type Application struct {
	Server  *httpserver.Server
	Store   *Store
	Service *DocumentService
}

func NewApplication(server *httpserver.Server, store *Store, service *DocumentService) *Application {
	return &Application{Server: server, Store: store, Service: service}
}

func NewErrorMapper() *pfwhttp.Mapper {
	rules := securityhttp.ErrorRules()
	rules = append(rules,
		pfwhttp.Is(ErrDocumentNotFound, pfwhttp.NewError(404, "not_found", "document not found")),
	)
	return pfwhttp.NewMapper(rules...)
}

func NewSecurityMiddleware(
	extractor securityhttp.CredentialExtractor[SessionCredentials],
	authenticator security.Authenticator[SessionCredentials, VerifiedSession],
	resolver security.PrincipalResolver[VerifiedSession, AppPrincipal],
	mapper pfwhttp.ErrorMapper,
) (*securityhttp.Middleware[SessionCredentials, VerifiedSession, AppPrincipal], error) {
	return securityhttp.NewMiddleware(extractor, authenticator, resolver, mapper)
}

type DocumentController struct{ service *DocumentService }

func NewDocumentController(service *DocumentService) *DocumentController {
	return &DocumentController{service: service}
}

func (c *DocumentController) UpdateTitle(w http.ResponseWriter, r *http.Request) error {
	input, err := pfwhttp.DecodeJSON[struct {
		Title string `json:"title"`
	}](r, pfwhttp.DecodeOptions{})
	if err != nil {
		return err
	}
	if err := c.service.UpdateTitle(r.Context(), r.PathValue("id"), input.Title); err != nil {
		return err
	}
	return pfwhttp.NoContent().Write(w)
}

func NewHTTPServer(
	controller *DocumentController,
	middleware *securityhttp.Middleware[SessionCredentials, VerifiedSession, AppPrincipal],
	mapper pfwhttp.ErrorMapper,
) *http.Server {
	adapter := pfwhttp.NewHandlerAdapter(mapper, nil)
	mux := http.NewServeMux()
	mux.Handle("PATCH /documents/{id}", middleware.Required(adapter.Wrap(controller.UpdateTitle)))
	mux.Handle("GET /me", middleware.Optional(adapter.Wrap(func(w http.ResponseWriter, r *http.Request) error {
		principal, err := security.Current[AppPrincipal](r.Context())
		if errors.Is(err, security.ErrUnauthenticated) {
			return pfwhttp.OK(map[string]bool{"authenticated": false}).Write(w)
		}
		if err != nil {
			return err
		}
		return pfwhttp.OK(principal).Write(w)
	})))
	// Cookie authentication needs cross-origin protection for state-changing calls.
	protection := http.NewCrossOriginProtection()
	protection.SetDenyHandler(adapter.Wrap(func(http.ResponseWriter, *http.Request) error { return security.ErrForbidden }))
	return &http.Server{Addr: "127.0.0.1:8080", Handler: protection.Handler(mux)}
}

func run() error {
	app, err := Initialize()
	if err != nil {
		return err
	}
	return lifecycle.New(lifecycle.Options{}, app.Server).RunSignals()
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
