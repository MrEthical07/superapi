package main

import (
	"bytes"
	"errors"
	"testing"

	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/core/db"
	"github.com/MrEthical07/superapi/internal/core/logx"
)

type fakeRunner struct {
	upCalled      bool
	downCalled    bool
	versionCalled bool
	forceCalled   bool
	downSteps     int
	forceVersion  int
}

func (f *fakeRunner) Up() (bool, error) {
	f.upCalled = true
	return false, nil
}

func (f *fakeRunner) Down(steps int) (bool, error) {
	f.downCalled = true
	f.downSteps = steps
	return false, nil
}

func (f *fakeRunner) Version() (db.MigrationVersion, error) {
	f.versionCalled = true
	return db.MigrationVersion{HasVersion: false}, nil
}

func (f *fakeRunner) Force(version int) error {
	f.forceCalled = true
	f.forceVersion = version
	return nil
}

func (f *fakeRunner) Close() error { return nil }

func TestParseCLI_UnknownCommand(t *testing.T) {
	_, err := parseCLI([]string{"nope"})
	if err == nil {
		t.Fatalf("expected parse error")
	}
}

func TestParseCLI_DownStepsValidation(t *testing.T) {
	_, err := parseCLI([]string{"down", "--steps=0"})
	if err == nil {
		t.Fatalf("expected steps validation error")
	}
}

func TestParseCLI_ForceVersionRequired(t *testing.T) {
	_, err := parseCLI([]string{"force"})
	if err == nil {
		t.Fatalf("expected version validation error")
	}
}

func TestExecuteCommandDispatch(t *testing.T) {
	logger, err := logx.New(logx.Config{Level: "info", Format: "json"})
	if err != nil {
		t.Fatalf("logger init failed: %v", err)
	}

	r := &fakeRunner{}
	if err := executeCommand(cliCommand{action: actionUp}, r, logger); err != nil {
		t.Fatalf("execute up: %v", err)
	}
	if !r.upCalled {
		t.Fatalf("expected up dispatch")
	}

	if err := executeCommand(cliCommand{action: actionDown, steps: 2}, r, logger); err != nil {
		t.Fatalf("execute down: %v", err)
	}
	if !r.downCalled || r.downSteps != 2 {
		t.Fatalf("expected down dispatch with steps")
	}

	if err := executeCommand(cliCommand{action: actionVersion}, r, logger); err != nil {
		t.Fatalf("execute version: %v", err)
	}
	if !r.versionCalled {
		t.Fatalf("expected version dispatch")
	}

	if err := executeCommand(cliCommand{action: actionForce, version: 3}, r, logger); err != nil {
		t.Fatalf("execute force: %v", err)
	}
	if !r.forceCalled || r.forceVersion != 3 {
		t.Fatalf("expected force dispatch with version")
	}
}

func TestRun_Help(t *testing.T) {
	var out bytes.Buffer
	code := run([]string{"--help"}, &out, &bytes.Buffer{}, runDeps{})
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if out.Len() == 0 {
		t.Fatalf("expected usage output")
	}
}

func TestRun_FailsWhenPostgresDisabled(t *testing.T) {
	deps := runDeps{
		loadConfig: func() (*config.Config, error) {
			cfg, err := config.Load()
			if err != nil {
				return nil, err
			}
			cfg.Postgres.Enabled = false
			cfg.Postgres.URL = ""
			return cfg, nil
		},
		newLogger: logx.New,
		newRunner: func(dbURL, sourceURL string) (runner, error) {
			return nil, errors.New("should not create runner")
		},
	}

	code := run([]string{"up"}, &bytes.Buffer{}, &bytes.Buffer{}, deps)
	if code == 0 {
		t.Fatalf("expected non-zero exit code")
	}
}
