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
	// NameContains, when set, also deletes every file or directory in the
	// repository whose base name contains it (case-insensitive). A feature
	// names its own files after itself so pruning needs no per-file list.
	NameContains string
	// TouchesSQL means pruning changes db/ and sqlc output must be regenerated.
	TouchesSQL bool
	// SQLRegenRequired means the checked-in generated code selects columns the
	// pruned schema no longer has, so the project compiles but fails at run
	// time until sqlc is regenerated. Init fails loudly when it cannot do it.
	SQLRegenRequired bool
}

// features lists every optional part of the template, in the order they are
// applied. Keep this in sync with docs/trim-to-what-you-need.md.
var features = []feature{
	{
		Flag:   "no-tenancy",
		Marker: "tenancy",
		Help:   "remove multi-tenancy: internal/tenancy, its migration, schema, queries, docs and TENANCY_* config",
		Paths: []string{
			"internal/tenancy",
		},
		// Every file or directory whose name contains this is deleted too: the
		// migration, sqlc schema, queries and generated code, the analyzer and
		// scaffolder extensions, the registration file, docs and workflows.
		NameContains:     "tenancy",
		TouchesSQL:       true,
		SQLRegenRequired: true,
	},
	{
		Flag:   "no-webauthn",
		Marker: "webauthn",
		Help:   "remove the WebAuthn credential store, ceremonies and WEBAUTHN_* config (also strips the WebAuthn blocks from the baseline migration)",
		Paths: []string{
			"db/schema/webauthn_credentials.sql",
			"db/queries/webauthn_credentials.sql",
			"internal/core/db/sqlcgen/webauthn_credentials.sql.go",
			"internal/core/auth/webauthn_repository.go",
			"internal/core/auth/provider_webauthn.go",
			"internal/core/auth/config_webauthn.go",
			"internal/core/auth/config_webauthn_test.go",
			"internal/core/config/webauthn.go",
			"internal/core/config/webauthn_test.go",
			"internal/tenancy/provider_webauthn.go",
			"internal/tenancy/provider_webauthn_test.go",
			"internal/modules/auth/webauthn.go",
			"internal/modules/auth/webauthn_test.go",
			"internal/modules/auth/stepup_webauthn_test.go",
			"docs/enabling-webauthn.md",
		},
		TouchesSQL: true,
	},
	{
		Flag:   "no-smtp",
		Marker: "smtp",
		Help:   "remove the built-in SMTP notifier and the SMTP_* / NOTIFY_*_URL config",
		Paths: []string{
			"internal/core/notify/smtp.go",
			"internal/core/notify/smtp_test.go",
			"internal/core/notify/fakesmtp_test.go",
			"internal/core/config/notify_smtp.go",
			"internal/core/config/notify_smtp_test.go",
		},
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
		Flag:   "no-rotate-tool",
		Marker: "rotate-tool",
		Help:   "remove the TOTP key rotation command (cmd/rotatetotpkey, make rotate-totp-key)",
		Paths: []string{
			"cmd/rotatetotpkey",
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

// initGlobs are path patterns (relative, slash separated) deleted with the init
// tooling unless --keep-init: the CI that only matters for developing the
// template itself. Every such file is named template-*.
var initGlobs = []string{
	".github/workflows/template-*",
	".github/template-*",
}
