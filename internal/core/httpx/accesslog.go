package httpx

import (
	"bufio"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/core/logx"
)

const (
	fnv64Offset = 14695981039346656037
	fnv64Prime  = 1099511628211
)

func AccessLog(cfg config.AccessLogConfig, log *logx.Logger) func(http.Handler) http.Handler {
	if !cfg.Enabled || log == nil {
		return func(next http.Handler) http.Handler { return next }
	}

	excludes := make(map[string]struct{}, len(cfg.ExcludePaths))
	for _, p := range cfg.ExcludePaths {
		excludes[p] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, skip := excludes[r.URL.Path]; skip {
				next.ServeHTTP(w, r)
				return
			}

			aw := &accessLogResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(aw, r)
			duration := time.Since(start)

			requestID := RequestIDFromContext(r.Context())
			statusCode := aw.statusCode
			route := RoutePattern(r, statusCode, aw.routePattern)

			shouldLogSlow := cfg.SlowThreshold > 0 && duration >= cfg.SlowThreshold
			alwaysLog := statusCode >= http.StatusInternalServerError || shouldLogSlow
			sampled := shouldSampleRequest(requestID, cfg.SampleRate)
			if !alwaysLog && !sampled {
				return
			}

			event := log.Info()
			switch {
			case statusCode >= http.StatusInternalServerError:
				event = log.Error()
			case shouldLogSlow || statusCode >= http.StatusBadRequest:
				event = log.Warn()
			}

			event = event.
				Str("method", r.Method).
				Str("route", route).
				Int("status", statusCode).
				Int64("duration_ms", duration.Milliseconds()).
				Int64("bytes_written", aw.bytesWritten).
				Bool("sampled", sampled)

			if requestID != "" {
				event = event.Str("request_id", requestID)
			}
			if cfg.IncludeUserAgent {
				event = event.Str("user_agent", r.UserAgent())
			}
			if cfg.IncludeRemoteIP {
				event = event.Str("remote_ip", remoteIP(r.RemoteAddr))
			}

			event.Msg("request")
		})
	}
}

func shouldSampleRequest(requestID string, sampleRate float64) bool {
	if sampleRate >= 1 {
		return true
	}
	if sampleRate <= 0 {
		return false
	}
	if requestID == "" {
		return false
	}

	hash := fnv64aString(requestID)
	threshold := uint64(sampleRate * float64(^uint64(0)))
	return hash <= threshold
}

func fnv64aString(s string) uint64 {
	h := uint64(fnv64Offset)
	for index := 0; index < len(s); index++ {
		h ^= uint64(s[index])
		h *= fnv64Prime
	}
	return h
}

func remoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err != nil {
		return strings.TrimSpace(remoteAddr)
	}
	return host
}

type accessLogResponseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int64
	routePattern string
}

func (w *accessLogResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *accessLogResponseWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytesWritten += int64(n)
	return n, err
}

func (w *accessLogResponseWriter) SetRoutePattern(pattern string) {
	w.routePattern = pattern
	if setter, ok := w.ResponseWriter.(interface{ SetRoutePattern(string) }); ok {
		setter.SetRoutePattern(pattern)
	}
}

func (w *accessLogResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *accessLogResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

func (w *accessLogResponseWriter) Push(target string, opts *http.PushOptions) error {
	pusher, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, opts)
}
