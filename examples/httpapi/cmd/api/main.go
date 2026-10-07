package main

import (
	"log"
	"time"

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
	server, err := bootstrap.Initialize(config.Config{HTTPAddress: ":8080", SeedUserName: "Ada"})
	if err != nil {
		return err
	}
	return lifecycle.New(lifecycle.Options{ShutdownTimeout: 5 * time.Second}, httpserver.New(server)).RunSignals()
}
