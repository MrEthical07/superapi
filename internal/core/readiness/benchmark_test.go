package readiness

import (
	"context"
	"testing"
	"time"
)

func BenchmarkServiceCheck_AllReady(b *testing.B) {
	svc := NewService()
	svc.Add("postgres", true, 50*time.Millisecond, func(ctx context.Context) error { return nil })
	svc.Add("redis", true, 50*time.Millisecond, func(ctx context.Context) error { return nil })

	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		report := svc.Check(ctx)
		if report.Status != StatusReady {
			b.Fatalf("status=%q want=%q", report.Status, StatusReady)
		}
	}
}

func BenchmarkServiceCheck_OneDependencyFailing(b *testing.B) {
	svc := NewService()
	svc.Add("postgres", true, 50*time.Millisecond, func(ctx context.Context) error { return nil })
	svc.Add("redis", true, 50*time.Millisecond, func(ctx context.Context) error { return context.DeadlineExceeded })

	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		report := svc.Check(ctx)
		if report.Status != StatusNotReady {
			b.Fatalf("status=%q want=%q", report.Status, StatusNotReady)
		}
	}
}
