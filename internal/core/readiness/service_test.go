package readiness

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCheck_AllDisabled(t *testing.T) {
	svc := NewService()
	svc.Add("postgres", false, time.Second, nil)
	svc.Add("redis", false, time.Second, nil)

	report := svc.Check(context.Background())
	if report.Status != StatusReady {
		t.Fatalf("status = %q, want %q", report.Status, StatusReady)
	}
	if report.Dependencies["postgres"].Status != DependencyDisabled {
		t.Fatalf("postgres status = %q, want %q", report.Dependencies["postgres"].Status, DependencyDisabled)
	}
	if report.Dependencies["redis"].Status != DependencyDisabled {
		t.Fatalf("redis status = %q, want %q", report.Dependencies["redis"].Status, DependencyDisabled)
	}
}

func TestCheck_EnabledAndHealthy(t *testing.T) {
	svc := NewService()
	svc.Add("postgres", true, time.Second, func(ctx context.Context) error { return nil })

	report := svc.Check(context.Background())
	if report.Status != StatusReady {
		t.Fatalf("status = %q, want %q", report.Status, StatusReady)
	}
	if report.Dependencies["postgres"].Status != DependencyOK {
		t.Fatalf("postgres status = %q, want %q", report.Dependencies["postgres"].Status, DependencyOK)
	}
}

func TestCheck_EnabledAndUnhealthy(t *testing.T) {
	svc := NewService()
	svc.Add("redis", true, time.Second, func(ctx context.Context) error { return errors.New("boom") })

	report := svc.Check(context.Background())
	if report.Status != StatusNotReady {
		t.Fatalf("status = %q, want %q", report.Status, StatusNotReady)
	}
	if report.Dependencies["redis"].Status != DependencyError {
		t.Fatalf("redis status = %q, want %q", report.Dependencies["redis"].Status, DependencyError)
	}
	if report.Dependencies["redis"].Message == "" {
		t.Fatalf("expected sanitized error message")
	}
}
