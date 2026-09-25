package main

// feature describes one prunable part of the template.
type feature struct {
	// Flag is the command-line switch that prunes it (for example no-tenancy).
	Flag string
	// Marker is the name used in template:begin/end blocks.
	Marker string
	// Help is shown in --help.
	Help string
	// Paths are files or directories deleted when the feature is pruned.
	Paths []string
	// TouchesSQL means pruning changes db/ and sqlc output must be regenerated.
	TouchesSQL bool
}

// features lists every optional part of the template, in the order they are
// applied. Keep this in sync with docs/trim-to-what-you-need.md.
var features = []feature{
	{
		Flag:   "no-tenancy",
		Marker: "tenancy",
		Help:   "remove tenant resolution, the tenants table and TENANCY_* config (tenancy stays off)",
		Paths: []string{
			"internal/core/tenant/resolver.go",
			"internal/core/tenant/resolver_test.go",
			"internal/core/tenant/directory.go",
			"internal/core/config/tenancy_test.go",
			"internal/modules/auth/tenancy_http_test.go",
			"cmd/createuser/tenant.go",
			"db/migrations/000002_tenants.up.sql",
			"db/migrations/000002_tenants.down.sql",
			"db/schema/tenants.sql",
			"db/queries/tenants.sql",
			"internal/core/db/sqlcgen/tenants.sql.go",
			"docs/multi-tenancy.md",
		},
		TouchesSQL: true,
	},
	{
		Flag:   "no-webauthn",
		Marker: "webauthn",
		Help:   "remove the WebAuthn credential store, ceremonies and WEBAUTHN_* config",
		Paths: []string{
			"db/migrations/000004_webauthn_credentials.up.sql",
			"db/migrations/000004_webauthn_credentials.down.sql",
			"db/schema/webauthn_credentials.sql",
			"db/queries/webauthn_credentials.sql",
			"internal/core/db/sqlcgen/webauthn_credentials.sql.go",
			"internal/core/auth/webauthn_repository.go",
			"internal/core/auth/provider_webauthn.go",
			"internal/core/auth/config_webauthn.go",
			"internal/modules/auth/webauthn.go",
			"internal/modules/auth/webauthn_test.go",
			"docs/enabling-webauthn.md",
		},
		TouchesSQL: true,
	},
	{
		Flag:   "no-document-store",
		Marker: "document-store",
		Help:   "remove the optional document (NoSQL) store package",
		Paths: []string{
			"internal/storage/document",
			"docs/document-store.md",
		},
	},
	{
		Flag:   "no-devx",
		Marker: "devx",
		Help:   "remove the module scaffolder (make module) and module SQL sync",
		Paths: []string{
			"cmd/modulegen",
			"cmd/modulesync",
			"internal/devx",
		},
	},
	{
		Flag:   "no-perf",
		Marker: "perf",
		Help:   "remove load-testing tooling (performance/, cmd/perftoken, make load-*)",
		Paths: []string{
			"performance",
			"cmd/perftoken",
			"docs/performance-testing.md",
		},
	},
	{
		Flag:   "no-demo",
		Marker: "demo",
		Help:   "remove bundled example code (document store audit example)",
		Paths: []string{
			"internal/storage/document/example",
		},
	},
}

// Markers that are not user-selectable.
const (
	// markerMaintainer wraps template-maintainer-only content (release
	// checklists, badges, showcase). Always removed by init.
	markerMaintainer = "maintainer"
	// markerInit wraps the init tooling itself (make init, CI job, README
	// step). Removed unless --keep-init.
	markerInit = "init"
)

// initPaths are deleted with the init tooling unless --keep-init.
var initPaths = []string{
	"cmd/templateinit",
}
