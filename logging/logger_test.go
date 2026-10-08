package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/palma99/palma-framework/logging"
)

func TestLevelsAndBoundFields(t *testing.T) {
	var output bytes.Buffer
	global := slog.Default()
	logger := logging.New(logging.Options{Output: &output, JSON: true})
	bound := logger.With("environment", "uat")
	bound.Debug(context.Background(), "hidden")
	bound.Info(context.Background(), "server started", "address", "127.0.0.1:8080")
	logger.Warn(context.Background(), "warning")
	logger.Error(context.Background(), "failure", "attempt", 2)
	if slog.Default() != global {
		t.Fatal("global logger changed")
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("level filtering: %s", output.String())
	}
	for index, line := range lines {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if event["level"] != []string{"INFO", "WARN", "ERROR"}[index] {
			t.Fatalf("level: %v", event)
		}
		if index == 0 && (event["environment"] != "uat" || event["address"] != "127.0.0.1:8080" || event["msg"] != "server started") {
			t.Fatalf("fields: %v", event)
		}
		if index > 0 && event["environment"] != nil {
			t.Fatalf("With modified parent: %v", event)
		}
	}
}

func TestDebugAndConcurrentLogging(t *testing.T) {
	var output bytes.Buffer
	logger := logging.New(logging.Options{Output: &output, Level: slog.LevelDebug, JSON: true})
	var workers sync.WaitGroup
	for index := 0; index < 20; index++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			logger.With("worker", index).Debug(context.Background(), "work")
		}(index)
	}
	workers.Wait()
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 20 {
		t.Fatalf("lost events: %d", len(lines))
	}
	for _, line := range lines {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if event["level"] != "DEBUG" || event["worker"] == nil {
			t.Fatalf("event: %v", event)
		}
	}
}
