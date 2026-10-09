package generate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenericSecurityBindingsGenerateAndRejectIncompatibleTypes(t *testing.T) {
	dir := fixture(t, map[string]string{
		"model/model.go": `package model
type Credentials string
type Identity string
type Principal struct { ID string }
`,
		"app.go": `package app
import (
 "context"
 "net/http"
 m "example.test/app/model"
 s "github.com/palma99/palma-framework/security"
 sh "github.com/palma99/palma-framework/security/http"
)
type Extractor struct{}
func NewExtractor()*Extractor{return &Extractor{}}
func(*Extractor) Extract(r *http.Request)(m.Credentials,error){return m.Credentials(r.Header.Get("Session")),nil}
type Authenticator struct{}
func NewAuthenticator()*Authenticator{return &Authenticator{}}
func(*Authenticator) Authenticate(ctx context.Context,c m.Credentials)(m.Identity,error){return m.Identity(c),nil}
type Resolver struct{}
func NewResolver()*Resolver{return &Resolver{}}
func(*Resolver) Resolve(ctx context.Context,i m.Identity)(m.Principal,error){return m.Principal{ID:string(i)},nil}
type WrongAuthenticator struct{}
func(*WrongAuthenticator) Authenticate(ctx context.Context,c string)(string,error){return c,nil}
func NewMiddleware(e sh.CredentialExtractor[m.Credentials],a s.Authenticator[m.Credentials,m.Identity],r s.PrincipalResolver[m.Identity,m.Principal])(*sh.Middleware[m.Credentials,m.Identity,m.Principal],error){return sh.NewMiddleware(e,a,r,nil)}
`,
		"compose.go": `//go:build pfw_inject
package app
import (
 p "github.com/palma99/palma-framework"
 s "github.com/palma99/palma-framework/security"
 sh "github.com/palma99/palma-framework/security/http"
 m "example.test/app/model"
)
func Initialize()(*sh.Middleware[m.Credentials,m.Identity,m.Principal],error){
 return p.Build[*sh.Middleware[m.Credentials,m.Identity,m.Principal]](
  p.Constructors(NewExtractor,NewAuthenticator,NewResolver,NewMiddleware),
  p.Bind[sh.CredentialExtractor[m.Credentials],*Extractor](),
  p.Bind[s.Authenticator[m.Credentials,m.Identity],*Authenticator](),
  p.Bind[s.PrincipalResolver[m.Identity,m.Principal],*Resolver](),
 )
}
`,
		"app_test.go": `package app
import (
 "net/http/httptest"
 "testing"
 m "example.test/app/model"
 s "github.com/palma99/palma-framework/security"
)
func TestGeneratedSecurity(t *testing.T){
 middleware,err:=Initialize();if err!=nil{t.Fatal(err)}
 request:=httptest.NewRequest("GET","/",nil);request.Header.Set("Session","user")
 request,err=middleware.RequiredRequest(request);if err!=nil{t.Fatal(err)}
 principal,err:=s.Current[m.Principal](request.Context());if err!=nil||principal.ID!="user"{t.Fatalf("principal: %+v %v",principal,err)}
}
`,
	})
	cfg := Config{Dir: dir}
	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	compileAndRun(t, dir, "-race")
	cfg.Check = true
	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "compose.go")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source = []byte(strings.Replace(string(source), "*Authenticator]()", "*WrongAuthenticator]()", 1))
	if err := os.WriteFile(path, source, 0644); err != nil {
		t.Fatal(err)
	}
	cfg.Check = false
	if _, err := Run(context.Background(), cfg); err == nil || !strings.Contains(err.Error(), "does not implement") {
		t.Fatalf("incompatible generic binding: %v", err)
	}
}
