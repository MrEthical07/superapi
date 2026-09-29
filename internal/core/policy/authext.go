package policy

import (
	"net/http"
	"sync"

	goauth "github.com/MrEthical07/goAuth"

	"github.com/MrEthical07/superapi/internal/core/auth"
)

// AuthExtension lets an optional feature take part in AuthRequired without the
// policy package knowing the feature exists.
type AuthExtension struct {
	// Attribute derives one feature-owned principal attribute from goAuth's
	// result (auth.AuthContext.Attributes). Return an empty key to add none.
	Attribute func(result *goauth.AuthResult) (key, value string)
	// Check runs after goAuth accepted the token and before the request
	// continues. A non-nil error rejects the request with the same 401 as an
	// invalid token, so the client cannot tell why.
	Check func(r *http.Request, result *goauth.AuthResult) error
}

// authExtensions maps an engine to the extensions its routes apply. It is
// written once at startup, before routes are registered, and only read after.
var authExtensions sync.Map // *goauth.Engine -> []AuthExtension

// UseAuthExtensions registers extensions for every AuthRequired policy built
// on engine. Call it once at startup, before any request is served (app.New
// does); it replaces earlier registrations for the engine.
func UseAuthExtensions(engine *goauth.Engine, exts ...AuthExtension) {
	if engine == nil {
		return
	}
	if len(exts) == 0 {
		authExtensions.Delete(engine)
		return
	}
	authExtensions.Store(engine, append([]AuthExtension(nil), exts...))
}

func authExtensionsFor(engine *goauth.Engine) []AuthExtension {
	v, ok := authExtensions.Load(engine)
	if !ok {
		return nil
	}
	exts, _ := v.([]AuthExtension)
	return exts
}

// applyAuthExtensions builds the principal attributes and runs the checks. It
// reports false when a check rejected the request.
func applyAuthExtensions(exts []AuthExtension, r *http.Request, result *goauth.AuthResult, principal *auth.AuthContext) bool {
	for _, ext := range exts {
		if ext.Attribute != nil {
			if key, value := ext.Attribute(result); key != "" {
				principal.Attributes = append(principal.Attributes, auth.Attribute{Key: key, Value: value})
			}
		}
	}
	for _, ext := range exts {
		if ext.Check != nil && ext.Check(r, result) != nil {
			return false
		}
	}
	return true
}
