package main

import (
	"context"
	pfw "github.com/palma99/palma-framework"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/palma99/palma-framework/examples/httpapi/internal/bootstrap"
	"github.com/palma99/palma-framework/examples/httpapi/internal/config"
	"github.com/palma99/palma-framework/lifecycle"
	"github.com/palma99/palma-framework/transport/httpserver"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	env, err := pfw.EnvironmentFromEnv(pfw.Local)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, err := config.Load(env)
	if err != nil {
		return err
	}
	server, cleanup, err := bootstrap.Initialize(ctx, env, cfg)
	if err != nil {
		return err
	}
	return lifecycle.New(lifecycle.Options{ShutdownTimeout: cfg.HTTP.ShutdownTimeout, Cleanup: cleanup}, httpserver.New(server)).Run(ctx)
}
