package main

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func TestRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantOutput string
	}{
		{name: "version", args: []string{"berth", "version"}, wantCode: 0, wantOutput: "berth version: not implemented\n"},
		{name: "lease list", args: []string{"berth", "lease", "list"}, wantCode: 0, wantOutput: "not implemented\n"},
		{name: "lease get", args: []string{"berth", "lease", "get", "example"}, wantCode: 0, wantOutput: "not implemented\n"},
		{name: "lease release", args: []string{"berth", "lease", "release", "example"}, wantCode: 0, wantOutput: "not implemented\n"},
		{name: "missing lease arg", args: []string{"berth", "lease", "get"}, wantCode: 1, wantOutput: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout := captureStdout(t)
			originalArgs := os.Args
			defer func() { os.Args = originalArgs }()
			os.Args = tt.args

			gotCode := run()
			gotOutput := stdout()

			if gotCode != tt.wantCode {
				t.Fatalf("code = %d, want %d", gotCode, tt.wantCode)
			}
			if gotOutput != tt.wantOutput {
				t.Fatalf("output = %q, want %q", gotOutput, tt.wantOutput)
			}
		})
	}
}

func captureStdout(t *testing.T) func() string {
	t.Helper()

	originalStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	os.Stdout = writer

	return func() string {
		_ = writer.Close()
		os.Stdout = originalStdout
		data, readErr := io.ReadAll(reader)
		if readErr != nil {
			t.Fatalf("ReadAll() error = %v", readErr)
		}
		return string(bytes.Clone(data))
	}
}
