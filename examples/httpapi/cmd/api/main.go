package main

import (
	"log"

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
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	server, err := bootstrap.Initialize(cfg)
	if err != nil {
		return err
	}
	return lifecycle.New(lifecycle.Options{ShutdownTimeout: cfg.HTTP.ShutdownTimeout}, httpserver.New(server)).RunSignals()
}
