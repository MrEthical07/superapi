package app

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/core/httpx"
	"github.com/MrEthical07/superapi/internal/core/logx"
)

type Module interface {
	Name() string
	Register(r httpx.Router) error
}

type App struct {
	cfg     *config.Config
	log     *logx.Logger
	server  *http.Server
	modules []Module
	router  *httpx.Mux
	deps    *Dependencies
}

func New(cfg *config.Config, log *logx.Logger, modules []Module) (*App, error) {
	if cfg == nil {
		return nil, errors.New("nil config")
	}
	if log == nil {
		return nil, errors.New("nil logger")
	}

	router := httpx.NewMux()
	deps, err := initDependencies(context.Background(), cfg)
	if err != nil {
		return nil, err
	}
	router.Use(httpx.CaptureRoutePattern)
	if deps.Metrics != nil && deps.Metrics.Enabled() {
		metricsHandler := deps.Metrics.Handler()
		if token := strings.TrimSpace(cfg.Metrics.AuthToken); token != "" {
			metricsHandler = requireBearerToken(metricsHandler, token)
		}
		router.Handle(http.MethodGet, deps.Metrics.Path(), metricsHandler)
	}

	var handler http.Handler = httpx.AssembleGlobalMiddleware(router, cfg.HTTP.Middleware, log, deps.Tracing)
	if deps.Metrics != nil {
		handler = deps.Metrics.InstrumentHTTP(handler)
	}

	srv := &http.Server{
		Addr:              cfg.HTTP.Addr,
		Handler:           handler,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
		MaxHeaderBytes:    cfg.HTTP.MaxHeaderBytes,
	}

	a := &App{
		cfg:     cfg,
		log:     log,
		server:  srv,
		modules: modules,
		router:  router,
		deps:    deps,
	}

	for _, m := range modules {
		if m == nil {
			continue
		}
		if binder, ok := m.(DependencyBinder); ok {
			binder.BindDependencies(a.deps)
		}
		if err := m.Register(a.router); err != nil {
			a.closeDependencies()
			return nil, err
		}
	}

	return a, nil
}

func requireBearerToken(next http.Handler, token string) http.Handler {
	token = strings.TrimSpace(token)
	if token == "" {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
		parts := strings.Fields(authHeader)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		provided := strings.TrimSpace(parts[1])
		if subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (a *App) Run(ctx context.Context) error {
	defer a.closeDependencies()

	errCh := make(chan error, 1)

	go func() {
		a.log.Info().
			Str("addr", a.cfg.HTTP.Addr).
			Str("service", a.cfg.ServiceName).
			Str("env", a.cfg.Env).
			Msg("starting http server")
		err := a.server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.HTTP.ShutdownTimeout)
		defer cancel()

		a.log.Info().Msg("shutdown initiated")
		if err := a.server.Shutdown(shutdownCtx); err != nil {
			a.log.Error().Err(err).Msg("shutdown error")
			return err
		}
		a.log.Info().Msg("shutdown complete")
		// Give server goroutine a chance to exit cleanly.
		select {
		case err := <-errCh:
			return err
		case <-time.After(500 * time.Millisecond):
			return nil
		}

	case err := <-errCh:
		return err
	}
}
