package main

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/palma99/palma-framework/security"
)

type SessionCredentials struct{ SessionID string }
type VerifiedSession struct{ UserID string }
type AppPrincipal struct {
	UserID         string   `json:"user_id"`
	OrganizationID string   `json:"organization_id"`
	Roles          []string `json:"roles"`
}

type SessionCookieExtractor struct{}

func NewSessionCookieExtractor() *SessionCookieExtractor { return &SessionCookieExtractor{} }
func (*SessionCookieExtractor) Extract(r *http.Request) (SessionCredentials, error) {
	cookie, err := r.Cookie("session")
	if errors.Is(err, http.ErrNoCookie) {
		return SessionCredentials{}, security.ErrMissingCredentials
	}
	if err != nil {
		return SessionCredentials{}, err
	}
	if cookie.Value == "" {
		return SessionCredentials{}, security.ErrInvalidCredentials
	}
	return SessionCredentials{SessionID: cookie.Value}, nil
}

type SessionAuthenticator struct{ sessions SessionRepository }

func NewSessionAuthenticator(sessions SessionRepository) *SessionAuthenticator {
	return &SessionAuthenticator{sessions: sessions}
}
func (a *SessionAuthenticator) Authenticate(ctx context.Context, c SessionCredentials) (VerifiedSession, error) {
	session, err := a.sessions.Find(ctx, c.SessionID)
	if errors.Is(err, ErrSessionNotFound) {
		return VerifiedSession{}, security.ErrInvalidCredentials
	}
	if err != nil {
		return VerifiedSession{}, err
	}
	if session.Revoked || !time.Now().Before(session.ExpiresAt) {
		return VerifiedSession{}, security.ErrInvalidCredentials
	}
	return VerifiedSession{UserID: session.UserID}, nil
}

type AppPrincipalResolver struct{ users UserRepository }

func NewAppPrincipalResolver(users UserRepository) *AppPrincipalResolver {
	return &AppPrincipalResolver{users: users}
}
func (r *AppPrincipalResolver) Resolve(ctx context.Context, identity VerifiedSession) (AppPrincipal, error) {
	user, err := r.users.GetUser(ctx, identity.UserID)
	if errors.Is(err, ErrUserNotFound) {
		return AppPrincipal{}, security.ErrUnauthenticated
	}
	if err != nil {
		return AppPrincipal{}, err
	}
	if user.Disabled {
		return AppPrincipal{}, security.ErrForbidden
	}
	return AppPrincipal{UserID: user.ID, OrganizationID: user.OrganizationID, Roles: append([]string(nil), user.Roles...)}, nil
}

const ActionDocumentUpdate = "document:update"

type DocumentAuthorizer struct{}

func NewDocumentAuthorizer() *DocumentAuthorizer { return &DocumentAuthorizer{} }
func (*DocumentAuthorizer) Check(ctx context.Context, p AppPrincipal, action string, d Document) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if action != ActionDocumentUpdate || d.OrganizationID != p.OrganizationID {
		return security.ErrForbidden
	}
	if slices.Contains(p.Roles, "admin") || d.OwnerID == p.UserID {
		return nil
	}
	return security.ErrForbidden
}

type DocumentService struct {
	documents  DocumentRepository
	authorizer security.Authorizer[AppPrincipal, Document]
}

func NewDocumentService(documents DocumentRepository, authorizer security.Authorizer[AppPrincipal, Document]) *DocumentService {
	return &DocumentService{documents: documents, authorizer: authorizer}
}
func (s *DocumentService) UpdateTitle(ctx context.Context, id, title string) error {
	principal, err := security.Current[AppPrincipal](ctx)
	if err != nil {
		return err
	}
	document, err := s.documents.GetDocument(ctx, id)
	if err != nil {
		return err
	}
	if err := s.authorizer.Check(ctx, principal, ActionDocumentUpdate, document); err != nil {
		return err
	}
	document.Title = title
	return s.documents.Save(ctx, document)
}
