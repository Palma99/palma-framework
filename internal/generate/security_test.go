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
 s "github.com/palma99/palma-framework/security"
 m "example.test/app/model"
 h "github.com/palma99/palma-framework/http"
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
func NewMapper() h.ErrorMapper { return h.NewMapper(sh.ErrorRules()...) }
type CustomMiddleware struct{}
func NewCustomMiddleware()*CustomMiddleware{return &CustomMiddleware{}}
func(*CustomMiddleware)Required(next http.Handler)http.Handler{return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){next.ServeHTTP(w,r.WithContext(s.WithPrincipal(r.Context(),m.Principal{ID:"custom"})))})}
func(c *CustomMiddleware)Optional(next http.Handler)http.Handler{return c.Required(next)}
`,
		"compose.go": `//go:build pfw_inject
package app
import (
 p "github.com/palma99/palma-framework"
 s "github.com/palma99/palma-framework/security"
 sh "github.com/palma99/palma-framework/security/http"
 m "example.test/app/model"
)
func Initialize()(sh.RequestAuthenticator,error){
 return p.Build[sh.RequestAuthenticator](
  p.Constructors(NewExtractor,NewAuthenticator,NewResolver,NewMapper,sh.NewMiddleware[m.Credentials,m.Identity,m.Principal]),
 p.AutoBind(),
  p.Bind[sh.CredentialExtractor[m.Credentials],*Extractor](),
  p.Bind[s.Authenticator[m.Credentials,m.Identity],*Authenticator](),
  p.Bind[s.PrincipalResolver[m.Identity,m.Principal],*Resolver](),
 )
}
func Bound()(sh.HandlerMiddleware,error){return p.Build[sh.HandlerMiddleware](
 p.Constructors(NewExtractor,NewAuthenticator,NewResolver,NewMapper,NewCustomMiddleware,sh.NewMiddleware[m.Credentials,m.Identity,m.Principal]),
 p.Bind[sh.HandlerMiddleware,*CustomMiddleware](), p.AutoBind(),
)}
`,
		"app_test.go": `package app
import (
 "net/http/httptest"
 "net/http"
 "testing"
 m "example.test/app/model"
 s "github.com/palma99/palma-framework/security"
)
func TestGeneratedSecurity(t *testing.T){
 middleware,err:=Initialize();if err!=nil{t.Fatal(err)}
 request:=httptest.NewRequest("GET","/",nil);request.Header.Set("Session","user")
 request,err=middleware.RequiredRequest(request);if err!=nil{t.Fatal(err)}
 principal,err:=s.Current[m.Principal](request.Context());if err!=nil||principal.ID!="user"{t.Fatalf("principal: %+v %v",principal,err)}
 custom,err:=Bound();if err!=nil{t.Fatal(err)}
 handler:=http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){p,err:=s.Current[m.Principal](r.Context());if err!=nil||p.ID!="custom"{t.Fatalf("custom principal: %+v %v",p,err)}})
 custom.Required(handler).ServeHTTP(httptest.NewRecorder(),httptest.NewRequest("GET","/",nil))
 custom.Optional(handler).ServeHTTP(httptest.NewRecorder(),httptest.NewRequest("GET","/",nil))
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
