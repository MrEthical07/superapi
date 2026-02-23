package app

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/core/httpx"
)

type Module interface {
	Name() string
	Register(r httpx.Router) error
}

type App struct {
	cfg     *config.Config
	server  *http.Server
	modules []Module
	router  *httpx.Mux
}

func New(cfg *config.Config, modules []Module) (*App, error) {
	if cfg == nil {
		return nil, errors.New("nil config")
	}

	router := httpx.NewMux()

	// Global middleware chain: keep this minimal and production-safe.
	var handler http.Handler = router
	handler = httpx.Recoverer(handler)
	handler = httpx.RequestID(handler)

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
		server:  srv,
		modules: modules,
		router:  router,
	}

	for _, m := range modules {
		if m == nil {
			continue
		}
		if err := m.Register(a.router); err != nil {
			return nil, err
		}
	}

	return a, nil
}

func (a *App) Run(ctx context.Context) error {
	errCh := make(chan error, 1)

	go func() {
		log.Printf("starting http server addr=%s service=%s env=%s", a.cfg.HTTP.Addr, a.cfg.ServiceName, a.cfg.Env)
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

		log.Printf("shutdown initiated")
		if err := a.server.Shutdown(shutdownCtx); err != nil {
			return err
		}
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
