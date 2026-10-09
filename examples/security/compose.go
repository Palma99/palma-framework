//go:build pfw_inject

package main

import (
	pfw "github.com/palma99/palma-framework"
	pfwhttp "github.com/palma99/palma-framework/http"
	"github.com/palma99/palma-framework/security"
	securityhttp "github.com/palma99/palma-framework/security/http"
	"github.com/palma99/palma-framework/transport/httpserver"
)

func Initialize() (*Application, error) {
	return pfw.Build[*Application](
		pfw.Constructors(NewStore, NewSessionCookieExtractor, NewSessionAuthenticator,
			NewAppPrincipalResolver, NewDocumentAuthorizer, NewDocumentService,
			NewErrorMapper, NewSecurityMiddleware, NewDocumentController, NewHTTPServer, httpserver.New, NewApplication),
		pfw.Bind[SessionRepository, *Store](),
		pfw.Bind[UserRepository, *Store](),
		pfw.Bind[DocumentRepository, *Store](),
		pfw.Bind[securityhttp.CredentialExtractor[SessionCredentials], *SessionCookieExtractor](),
		pfw.Bind[security.Authenticator[SessionCredentials, VerifiedSession], *SessionAuthenticator](),
		pfw.Bind[security.PrincipalResolver[VerifiedSession, AppPrincipal], *AppPrincipalResolver](),
		pfw.Bind[security.Authorizer[AppPrincipal, Document], *DocumentAuthorizer](),
		pfw.Bind[pfwhttp.ErrorMapper, *pfwhttp.Mapper](),
	)
}
