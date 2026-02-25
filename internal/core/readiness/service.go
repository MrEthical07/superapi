package readiness

import (
	"context"
	"errors"
	"time"
)

const (
	StatusReady    = "ready"
	StatusNotReady = "not_ready"

	DependencyOK       = "ok"
	DependencyDisabled = "disabled"
	DependencyError    = "error"
)

type DependencyStatus struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type Report struct {
	Status       string                      `json:"status"`
	Dependencies map[string]DependencyStatus `json:"dependencies"`
}

type CheckFunc func(context.Context) error

type dependency struct {
	enabled bool
	timeout time.Duration
	check   CheckFunc
}

type Service struct {
	deps map[string]dependency
}

func NewService() *Service {
	return &Service{deps: make(map[string]dependency)}
}

func (s *Service) Add(name string, enabled bool, timeout time.Duration, check CheckFunc) {
	s.deps[name] = dependency{enabled: enabled, timeout: timeout, check: check}
}

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
