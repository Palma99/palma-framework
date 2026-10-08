package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func runFixture(t *testing.T, source string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range map[string]string{"go.mod": "module example.test/runapp\n\ngo 1.26.0\n", "main.go": source} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestRunGeneratesReachableCompositionWithoutLayoutConvention(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := runFixture(t, `package main
import("fmt";"os";p "github.com/palma99/palma-framework";w "example.test/runapp/private/assembly")
func main(){env,err:=p.EnvironmentFromEnv("");if err!=nil{panic(err)};value,err:=w.Initialize(env);if err!=nil{panic(err)};fmt.Print(value);os.Exit(0)}
`)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.test/runapp\n\ngo 1.26.0\nrequire github.com/palma99/palma-framework v0.0.0\nreplace github.com/palma99/palma-framework => "+strconv.Quote(root)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	assembly := filepath.Join(dir, "private", "assembly")
	if err := os.MkdirAll(assembly, 0700); err != nil {
		t.Fatal(err)
	}
	for name, source := range map[string]string{
		"values.go":   "package assembly\nfunc Live()int{return 2}\nfunc Memory()int{return 1}\n",
		"anything.go": "//go:build pfw_inject\npackage assembly\nimport p \"github.com/palma99/palma-framework\"\nfunc Initialize(env p.Environment)(int,error){return p.Build[int](p.Environments(\"dev\",\"live\"),p.Constructors(Live),p.ForEnv(\"dev\",p.Override(p.Constructors(Memory))))}\n",
	} {
		if err := os.WriteFile(filepath.Join(assembly, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
	for _, env := range []string{"dev", "live"} {
		var out, diagnostics bytes.Buffer
		if err := runApplication(context.Background(), []string{"-env", env, "."}, &out, &diagnostics); err != nil {
			t.Fatalf("run: %v %s", err, diagnostics.String())
		}
		want := "1"
		if env == "live" {
			want = "2"
		}
		if out.String() != want {
			t.Fatalf("environment %s output=%q", env, out.String())
		}
		generated, err := os.ReadFile(filepath.Join(assembly, "pfw_gen.go"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(generated), "func Initialize(") != 1 || strings.Contains(string(generated), "_pfwInitialize") {
			t.Fatalf("run generated environment dispatch: %s", generated)
		}
		if strings.Contains(string(generated), "Memory()") != (env == "dev") || strings.Contains(string(generated), "Live()") != (env == "live") {
			t.Fatalf("run generated wrong graph for %s: %s", env, generated)
		}
	}
	if _, err := os.Stat(filepath.Join(assembly, "pfw_gen.go")); err != nil {
		t.Fatal("run did not generate the reachable initializer")
	}
}

func TestRunEnvironmentLayersArgumentsAndExitCode(t *testing.T) {
	dir := runFixture(t, `package main
import("fmt";"os")
func main(){fmt.Printf("%s|%s|%s|%s",os.Getenv("PFW_ENV"),os.Getenv("PFW_RUN_FILE_TEST"),os.Getenv("PFW_RUN_PROCESS_TEST"),os.Args[1]);os.Exit(7)}
`)
	t.Chdir(dir)
	for file, text := range map[string]string{".env": "PFW_RUN_FILE_TEST=base\n", ".env.local": "PFW_RUN_FILE_TEST=profile\nPFW_RUN_PROCESS_TEST=file\nPFW_ENV=wrong\n", ".env.local.local": "PFW_RUN_FILE_TEST=personal\n"} {
		if err := os.WriteFile(file, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PFW_ENV", "production")
	t.Setenv("PFW_RUN_PROCESS_TEST", "process")
	var out, diagnostics bytes.Buffer
	err := runApplication(context.Background(), []string{"-env", "local", ".", "--", "argument"}, &out, &diagnostics)
	var exit *applicationExit
	if !errors.As(err, &exit) || exit.code != 7 || out.String() != "local|personal|process|argument" {
		t.Fatalf("run: stdout=%q stderr=%q error=%v", out.String(), diagnostics.String(), err)
	}
	if os.Getenv("PFW_ENV") != "production" || os.Getenv("PFW_RUN_PROCESS_TEST") != "process" {
		t.Fatal("run changed the parent environment")
	}
}

type readyOutput struct {
	mu     sync.Mutex
	buffer bytes.Buffer
	once   sync.Once
	ready  chan struct{}
}

func (w *readyOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.buffer.Write(p)
	if strings.Contains(w.buffer.String(), "READY") {
		w.once.Do(func() { close(w.ready) })
	}
	return n, err
}

func TestRunForwardsCancellationAndWaitsForShutdown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process interrupt forwarding is Unix-specific")
	}
	dir := runFixture(t, `package main
import("fmt";"os";"os/signal")
func main(){signals:=make(chan os.Signal,1);signal.Notify(signals,os.Interrupt);fmt.Println("READY");<-signals;fmt.Println("STOPPED")}
`)
	t.Chdir(dir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := &readyOutput{ready: make(chan struct{})}
	var diagnostics bytes.Buffer
	finished := make(chan error, 1)
	go func() {
		finished <- runApplication(ctx, []string{"-env", "local", "-shutdown-timeout", "2s", "."}, out, &diagnostics)
	}()
	select {
	case <-out.ready:
	case err := <-finished:
		t.Fatalf("startup failed: %v %s", err, diagnostics.String())
	case <-time.After(30 * time.Second):
		t.Fatal("application did not become ready")
	}
	cancel()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("shutdown: %v %s", err, diagnostics.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not finish")
	}
	out.mu.Lock()
	defer out.mu.Unlock()
	if !strings.Contains(out.buffer.String(), "STOPPED") {
		t.Fatalf("no graceful shutdown: %s", out.buffer.String())
	}
}
