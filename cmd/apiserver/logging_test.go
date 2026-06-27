package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestParseLogLevel(t *testing.T) {
	cases := []struct {
		in      string
		want    slog.Level
		wantErr bool
	}{
		{"debug", slog.LevelDebug, false},
		{"info", slog.LevelInfo, false},
		{"warn", slog.LevelWarn, false},
		{"warning", slog.LevelWarn, false},
		{"error", slog.LevelError, false},
		{"INFO", slog.LevelInfo, false},   // case-insensitive
		{"Error", slog.LevelError, false}, // case-insensitive
		{"", 0, true},
		{"verbose", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseLogLevel(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseLogLevel(%q) = %v, want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseLogLevel(%q) unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("parseLogLevel(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestNewLogHandlerRejectsUnknownFormat(t *testing.T) {
	if _, err := newLogHandler(&bytes.Buffer{}, "yaml", slog.LevelInfo); err == nil {
		t.Fatal("newLogHandler with unknown format: want error, got nil")
	}
}

// TestSetupLoggingJSON verifies the JSON format produces machine-parseable
// output and that the level is honored (a debug line is suppressed at info).
func TestSetupLoggingJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := setupLogging(&buf, logFormatJSON, "info"); err != nil {
		t.Fatalf("setupLogging: %v", err)
	}
	slog.Info("hello", "k", "v")
	slog.Debug("suppressed at info level")

	lines := nonEmptyLines(buf.String())
	if len(lines) != 1 {
		t.Fatalf("got %d log lines, want 1 (debug must be filtered): %q", len(lines), buf.String())
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("log line is not valid JSON: %v\nline: %s", err, lines[0])
	}
	if rec["msg"] != "hello" || rec["k"] != "v" {
		t.Fatalf("unexpected JSON fields: %v", rec)
	}
}

// TestSetupLoggingTextOptOut confirms the text format remains available and is
// not JSON.
func TestSetupLoggingTextOptOut(t *testing.T) {
	var buf bytes.Buffer
	if err := setupLogging(&buf, logFormatText, "debug"); err != nil {
		t.Fatalf("setupLogging: %v", err)
	}
	slog.Debug("debugline", "k", "v")

	out := strings.TrimSpace(buf.String())
	if out == "" {
		t.Fatal("text handler at debug level produced no output")
	}
	if json.Valid([]byte(out)) {
		t.Fatalf("text format unexpectedly produced JSON: %q", out)
	}
	if !strings.Contains(out, "debugline") || !strings.Contains(out, "k=v") {
		t.Fatalf("text output missing expected fields: %q", out)
	}
}

func TestSetupLoggingRejectsBadConfig(t *testing.T) {
	if err := setupLogging(&bytes.Buffer{}, "json", "loud"); err == nil {
		t.Fatal("setupLogging with bad level: want error, got nil")
	}
	if err := setupLogging(&bytes.Buffer{}, "xml", "info"); err == nil {
		t.Fatal("setupLogging with bad format: want error, got nil")
	}
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}
