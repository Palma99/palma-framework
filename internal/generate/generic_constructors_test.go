package generate

import (
	"context"
	"testing"
)

func TestInstantiatedConstructorsCleanupAndOverride(t *testing.T) {
	dir := fixture(t, map[string]string{
		"app.go": `package app
import "fmt"
type Config struct{Fail bool}
type Box[T any] struct{Value string}
type App struct{Int *Box[int]; String *Box[string]}
var Closed []string
func NewBox[T any](cfg Config)(*Box[T],func()error,error){
 name:=fmt.Sprintf("%T",*new(T))
 close:=func()error{Closed=append(Closed,name);return nil}
 if cfg.Fail && name=="string" {return nil,close,fmt.Errorf("failed string")}
 return &Box[T]{Value:name},close,nil
}
func Alternate[T any]() *Box[T] {return &Box[T]{Value:"override"}}
func NewApp(i *Box[int],s *Box[string])*App{return &App{Int:i,String:s}}
`,
		"compose.go": `//go:build pfw_inject
package app
import p "github.com/palma99/palma-framework"
func Initialize(cfg Config)(*App,func()error,error){return p.BuildWithCleanup[*App](
 p.Constructors(NewBox[int],NewBox[string],NewBox[int],NewApp),
)}
func Replaced(cfg Config)(*App,func()error,error){return p.BuildWithCleanup[*App](
 p.Constructors(NewBox[int],NewBox[string],NewApp),
 p.Override(p.Constructors(Alternate[int])),
)}
`,
		"app_test.go": `package app
import("strings";"testing")
func TestInstances(t *testing.T){
 Closed=nil
 app,close,err:=Initialize(Config{});if err!=nil{t.Fatal(err)}
 if app.Int.Value!="int"||app.String.Value!="string"{t.Fatalf("instances: %+v",app)}
 if err:=close();err!=nil{t.Fatal(err)};close()
 if len(Closed)!=2{t.Fatalf("cleanup: %v",Closed)}
 Closed=nil
 app,close,err=Initialize(Config{Fail:true})
 if app!=nil||close!=nil||err==nil||!strings.Contains(err.Error(),"NewBox[string]"){t.Fatalf("failure: %v",err)}
 if len(Closed)!=2{t.Fatalf("failure cleanup: %v",Closed)}
 Closed=nil
 app,close,err=Replaced(Config{});if err!=nil{t.Fatal(err)}
 if app.Int.Value!="override"||app.String.Value!="string"{t.Fatal("override not selected")}
 close();if len(Closed)!=1||Closed[0]!="string"{t.Fatalf("overridden constructor ran: %v",Closed)}
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
}
