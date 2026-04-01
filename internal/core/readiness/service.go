package readiness

import (
	"context"
	"errors"
	"time"
)

const (
	// StatusReady indicates all enabled dependencies are healthy.
	StatusReady = "ready"
	// StatusNotReady indicates at least one enabled dependency is unhealthy.
	StatusNotReady = "not_ready"

	// DependencyOK indicates dependency health check succeeded.
	DependencyOK = "ok"
	// DependencyDisabled indicates dependency is intentionally disabled.
	DependencyDisabled = "disabled"
	// DependencyError indicates dependency health check failed.
	DependencyError = "error"
)

// DependencyStatus is one dependency health result in readiness report.
type DependencyStatus struct {
	// Status is one of ok, disabled, or error.
	Status string `json:"status"`
	// Message is a sanitized failure reason when status is error.
	Message string `json:"message,omitempty"`
}

// Report represents readiness status exposed by /readyz.
type Report struct {
	// Status is ready or not_ready.
	Status string `json:"status"`
	// Dependencies contains per-dependency health statuses.
	Dependencies map[string]DependencyStatus `json:"dependencies"`
}

// CheckFunc executes one dependency health check.
type CheckFunc func(context.Context) error

type dependency struct {
	enabled bool
	timeout time.Duration
	check   CheckFunc
}

// Service stores dependency checks and computes readiness reports.
type Service struct {
	deps map[string]dependency
}

// NewService constructs readiness service with empty dependency registry.
func NewService() *Service {
	return &Service{deps: make(map[string]dependency)}
}

// Add registers a named dependency check.
//
// When enabled is false, dependency is reported as disabled without running check.
func (s *Service) Add(name string, enabled bool, timeout time.Duration, check CheckFunc) {
	s.deps[name] = dependency{enabled: enabled, timeout: timeout, check: check}
}

// Check executes all registered dependency checks and returns readiness report.
func (s *Service) Check(ctx context.Context) Report {
	report := Report{
		Status:       StatusReady,
		Dependencies: make(map[string]DependencyStatus, len(s.deps)),
	}

	for name, dep := range s.deps {
		if !dep.enabled {
			report.Dependencies[name] = DependencyStatus{Status: DependencyDisabled}
			continue
		}

		if dep.check == nil {
			report.Status = StatusNotReady
			report.Dependencies[name] = DependencyStatus{Status: DependencyError, Message: "not configured"}
			continue
		}

		checkCtx, cancel := context.WithTimeout(ctx, dep.timeout)
		err := dep.check(checkCtx)
		cancel()

		if err != nil {
			report.Status = StatusNotReady
			report.Dependencies[name] = DependencyStatus{
				Status:  DependencyError,
				Message: sanitize(err),
			}
			continue
		}

		report.Dependencies[name] = DependencyStatus{Status: DependencyOK}
	}

	return report
}

func sanitize(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "unavailable"
}
