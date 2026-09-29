package app

import (
	"errors"
	"strings"
	"testing"

	goauth "github.com/MrEthical07/goAuth"

	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/config"
)

type stubFeature struct {
	name  string
	hooks *Hooks
	err   error
}

func (f stubFeature) Name() string                        { return f.name }
func (f stubFeature) Load(*config.Config) (*Hooks, error) { return f.hooks, f.err }

func wrapProvider(*Dependencies, *auth.StoreUserProvider) goauth.UserProvider { return nil }

func TestLoadFeatures(t *testing.T) {
	cfg := &config.Config{}

	got, err := loadFeatures(cfg, []Feature{
		nil,
		stubFeature{name: "off"},
		stubFeature{name: "on", hooks: &Hooks{Deprecations: []string{"old knob"}}},
	})
	if err != nil {
		t.Fatalf("loadFeatures: %v", err)
	}
	if len(got) != 1 || got[0].feature.Name() != "on" {
		t.Fatalf("only a feature that returned hooks is installed, got %d", len(got))
	}
	deps := &Dependencies{features: got}
	if w := deps.Deprecations(); len(w) != 1 || w[0] != "old knob" {
		t.Fatalf("deprecations = %v", w)
	}

	if _, err := loadFeatures(cfg, []Feature{stubFeature{name: "bad", err: errors.New("boom")}}); err == nil || !strings.Contains(err.Error(), "feature bad") {
		t.Fatalf("a Load error must name the feature, got %v", err)
	}

	if _, err := loadFeatures(cfg, []Feature{
		stubFeature{name: "a", hooks: &Hooks{UserProvider: wrapProvider}},
		stubFeature{name: "b", hooks: &Hooks{UserProvider: wrapProvider}},
	}); err == nil {
		t.Fatal("two features wrapping the user provider must be refused")
	}
}
