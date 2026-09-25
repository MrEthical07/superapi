package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseFlags(t *testing.T) {
	var stderr bytes.Buffer
	cases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "ok", args: []string{"--email", " a@example.com ", "--role", "admin"}},
		{name: "missing email", args: []string{}, wantErr: "--email is required"},
		{name: "positional password rejected", args: []string{"--email", "a@example.com", "hunter2"}, wantErr: "never passed as an argument"},
		{name: "bad tenant", args: []string{"--email", "a@example.com", "--tenant", "../x"}, wantErr: "invalid --tenant"},
		{name: "no password flag exists", args: []string{"--email", "a@example.com", "--password", "x"}, wantErr: "flag provided but not defined"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := parseFlags(tc.args, &stderr)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if opts.email != "a@example.com" || opts.role != "admin" {
					t.Fatalf("opts=%+v", opts)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err=%v want %q", err, tc.wantErr)
			}
		})
	}
}

func TestReadPasswordFromStdin(t *testing.T) {
	var stderr bytes.Buffer
	got, err := readPassword(true, strings.NewReader("s3cret pass\r\nignored\n"), &stderr)
	if err != nil || got != "s3cret pass" {
		t.Fatalf("got %q err=%v", got, err)
	}
	if _, err := readPassword(true, strings.NewReader("\n"), &stderr); err == nil {
		t.Fatal("empty password must be rejected")
	}
	// Not a terminal and no --password-stdin: refuse rather than read plainly.
	if _, err := readPassword(false, strings.NewReader("x\n"), &stderr); err == nil || !strings.Contains(err.Error(), "--password-stdin") {
		t.Fatalf("err=%v", err)
	}
	if strings.Contains(stderr.String(), "s3cret") {
		t.Fatal("password echoed to stderr")
	}
}
