package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	pfw "github.com/palma99/palma-framework"
	"github.com/palma99/palma-framework/config"
	"github.com/palma99/palma-framework/internal/generate"
)

var runCommand = command{usage: "run [-env name] [-env-dir directory] [-shutdown-timeout duration] [main-package] [-- app-args...]", run: runApplication}

type applicationExit struct{ code int }

func (e *applicationExit) Error() string {
	return fmt.Sprintf("application exited with status %d", e.code)
}

func runApplication(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("pfw run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	envFlag := flags.String("env", "", "environment (or PFW_ENV)")
	envDir := flags.String("env-dir", "", "dotenv directory (default module root)")
	grace := flags.Duration("shutdown-timeout", 30*time.Second, "maximum wait after forwarding a shutdown signal")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *grace <= 0 {
		return fmt.Errorf("shutdown-timeout must be positive")
	}
	env := pfw.Environment(*envFlag)
	explicit := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "env" {
			explicit = true
		}
	})
	if !explicit {
		var err error
		env, err = pfw.EnvironmentFromEnv("")
		if err != nil {
			return fmt.Errorf("select an environment with -env or PFW_ENV: %w", err)
		}
	}
	if err := env.Validate(); err != nil {
		return err
	}
	remaining := flags.Args()
	pattern := "."
	var appArgs []string
	if len(remaining) > 0 {
		pattern = remaining[0]
		appArgs = remaining[1:]
		if len(appArgs) > 0 && appArgs[0] == "--" {
			appArgs = appArgs[1:]
		}
	}
	prepare, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	entry, err := generate.FindEntry(prepare, "", pattern)
	if err != nil {
		stop()
		return err
	}
	if len(entry.Initializers) > 0 {
		if _, err := generate.Run(prepare, generate.Config{Dir: entry.Root, Patterns: entry.Initializers, Environment: string(env)}); err != nil {
			stop()
			return err
		}
	}
	directory := *envDir
	if directory == "" {
		directory = entry.Root
	}
	directory, err = filepath.Abs(directory)
	if err != nil {
		stop()
		return err
	}
	values, err := config.EnvironmentValues(env, directory)
	if err != nil {
		stop()
		return err
	}
	temp, err := os.MkdirTemp("", "pfw-run-")
	if err != nil {
		stop()
		return err
	}
	defer os.RemoveAll(temp)
	binary := filepath.Join(temp, "application")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.CommandContext(prepare, "go", "build", "-o", binary, entry.Package)
	build.Dir = entry.Root
	build.Stdout = stdout
	build.Stderr = stderr
	if err := build.Run(); err != nil {
		stop()
		return fmt.Errorf("build application: %w", err)
	}
	if err := prepare.Err(); err != nil {
		stop()
		return err
	}
	stop()
	child := exec.Command(binary, appArgs...)
	child.Dir = entry.Root
	child.Env = childEnvironment(values, env, directory)
	child.Stdin = os.Stdin
	child.Stdout = stdout
	child.Stderr = stderr
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	if err := child.Start(); err != nil {
		return err
	}
	finished := make(chan error, 1)
	go func() { finished <- child.Wait() }()
	var timer *time.Timer
	var deadline <-chan time.Time
	done := ctx.Done()
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	requestStop := func(sig os.Signal) {
		if timer != nil {
			_ = child.Process.Kill()
			return
		}
		_ = child.Process.Signal(sig)
		timer = time.NewTimer(*grace)
		deadline = timer.C
	}
	for {
		select {
		case sig := <-signals:
			requestStop(sig)
		case <-done:
			done = nil
			requestStop(os.Interrupt)
		case <-deadline:
			deadline = nil
			_ = child.Process.Kill()
		case err := <-finished:
			return applicationResult(err)
		}
	}
}

func applicationResult(err error) error {
	if exit, ok := err.(*exec.ExitError); ok {
		code := exit.ExitCode()
		if code < 0 {
			code = 1
			if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				code = 128 + int(status.Signal())
			}
		}
		return &applicationExit{code: code}
	}
	return err
}
func childEnvironment(files map[string]string, env pfw.Environment, directory string) []string {
	merged := make(map[string]string)
	for key, value := range files {
		merged[key] = value
	}
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			merged[key] = value
		}
	}
	merged["PFW_ENV"] = string(env)
	merged["PFW_ENV_DIR"] = directory
	var keys []string
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var result []string
	for _, key := range keys {
		result = append(result, key+"="+merged[key])
	}
	return result
}
