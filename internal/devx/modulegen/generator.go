package modulegen

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	registryImportsMarker = "// MODULE_IMPORTS"
	registryListMarker    = "// MODULE_LIST"
)

var validInputName = regexp.MustCompile(`^[a-z0-9_-]+$`)

// ModuleSpec is the normalized module naming information.
type ModuleSpec struct {
	// RawName is the original user-provided module name.
	RawName string
	// Package is the normalized Go package name.
	Package string
	// RoutePath is the normalized URL path segment.
	RoutePath string
}

// TemplateOptions toggles optional scaffolding features.
type TemplateOptions struct {
	// UseDB includes SQL scaffold files.
	UseDB bool
	// UseAuth includes AuthRequired policy wiring.
	UseAuth bool
	// UseTenant includes tenant policy wiring.
	UseTenant bool
	// UseRateLimit includes default rate-limit policy wiring.
	UseRateLimit bool
	// UseCache includes default cache-read policy wiring.
	UseCache bool
	// CreateMigration creates an initial migration scaffold.
	CreateMigration bool
}

// TemplateConfig combines normalized spec and generation options.
type TemplateConfig struct {
	// Spec contains normalized naming fields.
	Spec ModuleSpec
	// Options contains feature toggles for generated files.
	Options TemplateOptions
}

// NormalizeName validates and normalizes a module name into spec fields.
func NormalizeName(name string) (ModuleSpec, error) {
	input := strings.TrimSpace(name)
	raw := strings.ToLower(input)
	if raw == "" {
		return ModuleSpec{}, errors.New("module name is required")
	}
	if input != raw {
		return ModuleSpec{}, fmt.Errorf("invalid module name %q: use lowercase only (example: %q)", name, "project_tasks")
	}
	if !validInputName.MatchString(raw) {
		return ModuleSpec{}, fmt.Errorf("invalid module name %q: allowed characters are lowercase letters, digits, '-' and '_' (example: %q)", name, "project-tasks")
	}

	parts := splitParts(raw)
	if len(parts) == 0 {
		return ModuleSpec{}, fmt.Errorf("invalid module name %q: include at least one alphanumeric segment (example: %q)", name, "projects")
	}
	if parts[0] == "" || (parts[0][0] < 'a' || parts[0][0] > 'z') {
		return ModuleSpec{}, fmt.Errorf("invalid module name %q: must start with a letter (example: %q)", name, "projects")
	}

	pkg := strings.Join(parts, "_")
	route := strings.Join(parts, "-")
	return ModuleSpec{RawName: raw, Package: pkg, RoutePath: route}, nil
}

func splitParts(raw string) []string {
	replacer := strings.NewReplacer("-", "_", "__", "_")
	normalized := replacer.Replace(raw)
	fragments := strings.Split(normalized, "_")
	out := make([]string, 0, len(fragments))
	for _, fragment := range fragments {
		fragment = strings.TrimSpace(fragment)
		if fragment == "" {
			continue
		}
		out = append(out, fragment)
	}
	return out
}

// UpdateRegistry inserts module import and constructor entries in modules registry.
func UpdateRegistry(content, moduleImportPath, packageName string) (string, bool, error) {
	lines := strings.Split(content, "\n")
	importLine := fmt.Sprintf("\t\"%s\"", moduleImportPath)
	entryLine := fmt.Sprintf("\t\t%s.New(),", packageName)

	importsMarkerIndex := indexOfLineContaining(lines, registryImportsMarker)
	listMarkerIndex := indexOfLineContaining(lines, registryListMarker)
	if importsMarkerIndex < 0 || listMarkerIndex < 0 {
		return "", false, errors.New("module registry markers not found")
	}

	changed := false
	if !containsTrimmedLine(lines, importLine) {
		lines = insertLine(lines, importsMarkerIndex, importLine)
		changed = true
		importsMarkerIndex++
		if listMarkerIndex >= importsMarkerIndex {
			listMarkerIndex++
		}
	}
	if !containsTrimmedLine(lines, entryLine) {
		lines = insertLine(lines, listMarkerIndex, entryLine)
		changed = true
	}

	lines = sortRegion(lines, registryImportsMarker, func(line string) bool {
		return strings.Contains(line, "\"github.com/MrEthical07/superapi/internal/modules/")
	})
	lines = sortRegion(lines, registryListMarker, func(line string) bool {
		trimmed := strings.TrimSpace(line)
		return strings.HasSuffix(trimmed, ".New(),")
	})

	updated := strings.Join(lines, "\n")
	if content == updated {
		changed = false
	}
	return updated, changed, nil
}

func containsTrimmedLine(lines []string, target string) bool {
	for _, line := range lines {
		if strings.TrimSpace(line) == strings.TrimSpace(target) {
			return true
		}
	}
	return false
}

func indexOfLineContaining(lines []string, needle string) int {
	for i, line := range lines {
		if strings.Contains(line, needle) {
			return i
		}
	}
	return -1
}

func insertLine(lines []string, index int, line string) []string {
	if index < 0 || index > len(lines) {
		return lines
	}
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:index]...)
	out = append(out, line)
	out = append(out, lines[index:]...)
	return out
}

func sortRegion(lines []string, marker string, predicate func(string) bool) []string {
	markerIdx := indexOfLineContaining(lines, marker)
	if markerIdx < 0 {
		return lines
	}
	start := markerIdx - 1
	for start >= 0 && predicate(lines[start]) {
		start--
	}
	start++
	if start >= markerIdx {
		return lines
	}
	region := append([]string(nil), lines[start:markerIdx]...)
	sort.Slice(region, func(i, j int) bool {
		return strings.TrimSpace(region[i]) < strings.TrimSpace(region[j])
	})
	copy(lines[start:markerIdx], region)
	return lines
}

// RenderFiles renders module source files keyed by relative path.
func RenderFiles(cfg TemplateConfig) map[string]string {
	spec := cfg.Spec
	files := map[string]string{
		"module.go":       renderModuleFile(cfg),
		"routes.go":       renderRoutesFile(cfg),
		"dto.go":          renderDTOFile(spec),
		"handler.go":      renderHandlerFile(spec),
		"service.go":      renderServiceFile(cfg),
		"repo.go":         renderRepoFile(cfg),
		"handler_test.go": renderHandlerTestFile(cfg),
		"service_test.go": renderServiceTestFile(cfg),
	}
	if cfg.Options.UseDB {
		files[filepath.Join("db", "schema.sql")] = renderModuleSchema(spec)
		files[filepath.Join("db", "queries.sql")] = renderModuleQueries(spec)
	}
	return files
}

func renderModuleFile(cfg TemplateConfig) string {
	spec := cfg.Spec
	imports := []string{
		`"github.com/MrEthical07/superapi/internal/core/app"`,
		`"github.com/MrEthical07/superapi/internal/core/modulekit"`,
	}

	var b strings.Builder
	b.WriteString("package " + spec.Package + "\n\n")
	b.WriteString("import (\n")
	for _, imp := range imports {
		b.WriteString("\t" + imp + "\n")
	}
	b.WriteString(")\n\n")
	b.WriteString("type Module struct {\n")
	b.WriteString("\thandler *Handler\n")
	b.WriteString("\truntime modulekit.Runtime\n")
	b.WriteString("}\n\n")
	if cfg.Options.UseDB {
		// The repository is wired with the relational boundary in
		// BindDependencies; New() starts with a nil boundary until deps arrive.
		b.WriteString("func New() *Module {\n")
		b.WriteString("\treturn &Module{handler: NewHandler(NewService(NewRepo(nil)))}\n")
		b.WriteString("}\n\n")
	} else {
		b.WriteString("func New() *Module {\n")
		b.WriteString("\treturn &Module{handler: NewHandler(NewService(NewRepo()))}\n")
		b.WriteString("}\n\n")
	}
	b.WriteString("var _ app.Module = (*Module)(nil)\n")
	b.WriteString("var _ app.DependencyBinder = (*Module)(nil)\n\n")
	b.WriteString("func (m *Module) Name() string { return \"" + spec.Package + "\" }\n\n")
	b.WriteString("func (m *Module) BindDependencies(deps *app.Dependencies) {\n")
	b.WriteString("\tm.runtime = modulekit.New(deps)\n")
	if cfg.Options.UseDB {
		// Wire the relational boundary into the repository. runtime.DB() yields
		// the sqlc query boundary (nil when Postgres is disabled).
		b.WriteString("\tm.handler = NewHandler(NewService(NewRepo(m.runtime.DB())))\n")
	} else {
		b.WriteString("\tif m.handler == nil {\n")
		b.WriteString("\t\tm.handler = NewHandler(NewService(NewRepo()))\n")
		b.WriteString("\t}\n")
	}
	b.WriteString("}\n")
	return b.String()
}

func renderRoutesFile(cfg TemplateConfig) string {
	spec := cfg.Spec
	imports := []string{
		`"net/http"`,
		`"github.com/MrEthical07/superapi/internal/core/httpx"`,
	}
	if cfg.Options.UseAuth || cfg.Options.UseTenant || cfg.Options.UseRateLimit || cfg.Options.UseCache {
		imports = append(imports, `"github.com/MrEthical07/superapi/internal/core/policy"`)
	}
	if cfg.Options.UseCache || cfg.Options.UseRateLimit {
		imports = append(imports, `"time"`)
	}
	if cfg.Options.UseCache {
		imports = append(imports, `"github.com/MrEthical07/superapi/internal/core/cache"`)
	}
	if cfg.Options.UseRateLimit {
		imports = append(imports, `"github.com/MrEthical07/superapi/internal/core/ratelimit"`)
	}

	policies := routePolicies(cfg)

	var b strings.Builder
	b.WriteString("package " + spec.Package + "\n\n")
	b.WriteString("import (\n")
	for _, imp := range imports {
		b.WriteString("\t" + imp + "\n")
	}
	b.WriteString(")\n\n")
	b.WriteString("func (m *Module) Register(r httpx.Router) error {\n")
	b.WriteString("\tif m.handler == nil {\n")
	b.WriteString("\t\tm.handler = NewHandler(NewService(" + newRepoCall(cfg) + "))\n")
	b.WriteString("\t}\n\n")
	if cfg.Options.UseTenant {
		// modulegen cannot know the runtime tenancy flag; remind the operator.
		b.WriteString("\t// NOTE: TenantRequired() below needs TENANCY_ENABLED=true at runtime,\n")
		b.WriteString("\t// otherwise every request to this route is rejected (no tenant in context).\n")
		b.WriteString("\t// Remove the tenant policy (and --tenant) if this module is not tenant-scoped.\n")
	}
	if len(policies) == 0 {
		b.WriteString("\tr.Handle(http.MethodGet, \"/api/v1/" + spec.RoutePath + "/ping\", httpx.Adapter(m.handler.Ping))\n")
	} else {
		b.WriteString("\tr.Handle(\n")
		b.WriteString("\t\thttp.MethodGet,\n")
		b.WriteString("\t\t\"/api/v1/" + spec.RoutePath + "/ping\",\n")
		b.WriteString("\t\thttpx.Adapter(m.handler.Ping),\n")
		for _, line := range policies {
			b.WriteString(line + "\n")
		}
		b.WriteString("\t)\n")
	}
	b.WriteString("\n\treturn nil\n}\n")
	return b.String()
}

func routePolicies(cfg TemplateConfig) []string {
	spec := cfg.Spec
	opt := cfg.Options
	policies := make([]string, 0, 4)
	if opt.UseAuth {
		policies = append(policies, "\t\tpolicy.AuthRequired(m.runtime.AuthEngine(), m.runtime.AuthMode()),")
	}
	if opt.UseTenant {
		policies = append(policies, "\t\tpolicy.TenantRequired(),")
	}
	if opt.UseRateLimit {
		policies = append(policies, "\t\tpolicy.RateLimit(m.runtime.Limiter(), ratelimit.Rule{Limit: 30, Window: time.Minute, Scope: ratelimit.ScopeAuto}),")
	}
	if opt.UseCache {
		var cacheCfg strings.Builder
		cacheCfg.WriteString("cache.CacheReadConfig{")
		cacheCfg.WriteString("Key: \"" + spec.Package + ".ping\", ")
		cacheCfg.WriteString("TTL: 30 * time.Second")
		cacheCfg.WriteString(", TagSpecs: []cache.CacheTagSpec{{Name: \"" + spec.Package + ".ping\"")
		if opt.UseTenant {
			cacheCfg.WriteString(", TenantID: true")
		} else if opt.UseAuth {
			cacheCfg.WriteString(", UserID: true")
		}
		cacheCfg.WriteString("}}")
		if opt.UseAuth || opt.UseTenant {
			cacheCfg.WriteString(", AllowAuthenticated: true, VaryBy: cache.CacheVaryBy{")
			if opt.UseTenant {
				cacheCfg.WriteString("TenantID: true")
			} else {
				cacheCfg.WriteString("UserID: true")
			}
			cacheCfg.WriteString("}")
		}
		cacheCfg.WriteString("}")
		policies = append(policies, "\t\tpolicy.CacheRead(m.runtime.CacheManager(), "+cacheCfg.String()+"),")
	}
	return policies
}

func renderDTOFile(spec ModuleSpec) string {
	return "package " + spec.Package + "\n\n" +
		"type pingResponse struct {\n\tStatus string `json:\"status\"`\n\tModule string `json:\"module\"`\n}\n"
}

func renderHandlerFile(spec ModuleSpec) string {
	return "package " + spec.Package + "\n\n" +
		"import (\n\t\"github.com/MrEthical07/superapi/internal/core/httpx\"\n)\n\n" +
		"type Handler struct {\n\tsvc Service\n}\n\n" +
		"func NewHandler(svc Service) *Handler {\n\treturn &Handler{svc: svc}\n}\n\n" +
		"func (h *Handler) Ping(ctx *httpx.Context, req httpx.NoBody) (pingResponse, error) {\n" +
		"\treturn h.svc.Ping(), nil\n}\n"
}

func renderServiceFile(cfg TemplateConfig) string {
	spec := cfg.Spec
	if !cfg.Options.UseDB {
		return "package " + spec.Package + "\n\n" +
			"type Service interface {\n\tPing() pingResponse\n}\n\n" +
			"type service struct {\n\trepo *Repo\n}\n\n" +
			"func NewService(repo *Repo) Service {\n\treturn &service{repo: repo}\n}\n\n" +
			"func (s *service) Ping() pingResponse {\n\treturn pingResponse{Status: \"ok\", Module: \"" + spec.Package + "\"}\n}\n"
	}

	// DB-enabled service: owns the write transaction boundary via the repo's
	// storage boundary. Reads call the repo directly; writes wrap repo calls in
	// DB().WithTx(ctx, fn). This example keeps Ping and shows where a
	// transactional write would go.
	return "package " + spec.Package + "\n\n" +
		"type Service interface {\n\tPing() pingResponse\n}\n\n" +
		"type service struct {\n\trepo *Repo\n}\n\n" +
		"func NewService(repo *Repo) Service {\n\treturn &service{repo: repo}\n}\n\n" +
		"func (s *service) Ping() pingResponse {\n\treturn pingResponse{Status: \"ok\", Module: \"" + spec.Package + "\"}\n}\n\n" +
		"// Example write path (uncomment and adapt once you add queries):\n" +
		"//\n" +
		"// func (s *service) Create(ctx context.Context, in CreateInput) error {\n" +
		"// \treturn s.repo.pg.WithTx(ctx, func(txCtx context.Context) error {\n" +
		"// \t\treturn s.repo.Insert(txCtx, in)\n" +
		"// \t})\n" +
		"// }\n"
}

func renderHandlerTestFile(cfg TemplateConfig) string {
	spec := cfg.Spec
	return "package " + spec.Package + "\n\n" +
		"import (\n\t\"encoding/json\"\n\t\"net/http\"\n\t\"net/http/httptest\"\n\t\"testing\"\n\n\t\"github.com/MrEthical07/superapi/internal/core/httpx\"\n)\n\n" +
		"func TestPingHandler(t *testing.T) {\n" +
		"\th := NewHandler(NewService(" + newRepoCall(cfg) + "))\n\trr := httptest.NewRecorder()\n\treq := httptest.NewRequest(http.MethodGet, \"/api/v1/" + spec.RoutePath + "/ping\", nil)\n\n" +
		"\thandler := httpx.Adapter(h.Ping)\n\thandler.ServeHTTP(rr, req)\n\n" +
		"\tif rr.Code != http.StatusOK {\n\t\tt.Fatalf(\"status=%d want=%d\", rr.Code, http.StatusOK)\n\t}\n\n" +
		"\tvar body struct {\n\t\tOK   bool `json:\"ok\"`\n\t\tData pingResponse `json:\"data\"`\n\t}\n" +
		"\tif err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {\n\t\tt.Fatalf(\"unmarshal response: %v\", err)\n\t}\n" +
		"\tif !body.OK || body.Data.Module != \"" + spec.Package + "\" || body.Data.Status != \"ok\" {\n" +
		"\t\tt.Fatalf(\"unexpected response: %+v\", body)\n\t}\n}\n"
}

func renderServiceTestFile(cfg TemplateConfig) string {
	spec := cfg.Spec
	return "package " + spec.Package + "\n\n" +
		"import \"testing\"\n\n" +
		"func TestServicePing(t *testing.T) {\n" +
		"\ts := NewService(" + newRepoCall(cfg) + ")\n\tres := s.Ping()\n\tif res.Status != \"ok\" {\n\t\tt.Fatalf(\"status=%q want=%q\", res.Status, \"ok\")\n\t}\n\n" +
		"\tif res.Module != \"" + spec.Package + "\" {\n\t\tt.Fatalf(\"module=%q want=%q\", res.Module, \"" + spec.Package + "\")\n\t}\n}\n"
}

// newRepoCall returns the NewRepo(...) constructor call for generated code:
// DB-enabled modules pass a *storage.Postgres (nil in tests/bootstrap), others
// take no argument.
func newRepoCall(cfg TemplateConfig) string {
	if cfg.Options.UseDB {
		return "NewRepo(nil)"
	}
	return "NewRepo()"
}

func renderModuleSchema(spec ModuleSpec) string {
	return "-- Module-local sqlc schema source for " + spec.Package + ".\n" +
		"-- Keep module SQL here. `make sqlc-generate` syncs this file into db/schema/" + spec.Package + ".sql.\n" +
		"-- Example:\n" +
		"-- CREATE TABLE IF NOT EXISTS " + spec.RoutePath + " (\n" +
		"--     id TEXT PRIMARY KEY,\n" +
		"--     created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()\n" +
		"-- );\n"
}

func renderModuleQueries(spec ModuleSpec) string {
	return "-- Module-local sqlc queries for " + spec.Package + ".\n" +
		"-- Keep module queries here. `make sqlc-generate` syncs this file into db/queries/" + spec.Package + ".sql.\n" +
		"-- Example:\n" +
		"-- name: List" + toPascalCase(spec.Package) + " :many\n" +
		"-- SELECT id, created_at\n" +
		"-- FROM " + spec.RoutePath + ";\n"
}

func renderRepoFile(cfg TemplateConfig) string {
	pkg := cfg.Spec.Package
	if !cfg.Options.UseDB {
		// No DB: the repo takes no storage dependency.
		return "package " + pkg + "\n\n" +
			"type Repo struct{}\n\n" +
			"func NewRepo() *Repo {\n\treturn &Repo{}\n}\n"
	}

	// DB-enabled repo over the sqlc boundary. It holds *storage.Postgres and
	// obtains sqlc queries per call via pg.Queries(ctx), which binds to the
	// service-owned transaction when inside DB().WithTx. Add module queries in
	// db/queries.sql, run `make sqlc-generate`, then call
	// r.pg.Queries(ctx).<GeneratedMethod>(...) here and map rows to domain types.
	return "package " + pkg + "\n\n" +
		"import (\n" +
		"\t\"github.com/MrEthical07/superapi/internal/core/storage\"\n" +
		")\n\n" +
		"type Repo struct {\n\tpg *storage.Postgres\n}\n\n" +
		"func NewRepo(pg *storage.Postgres) *Repo {\n\treturn &Repo{pg: pg}\n}\n\n" +
		"// Example read (uncomment once you add queries and run make sqlc-generate):\n" +
		"//\n" +
		"// func (r *Repo) List(ctx context.Context) ([]Item, error) {\n" +
		"// \trows, err := r.pg.Queries(ctx).List" + toPascalCase(pkg) + "(ctx)\n" +
		"// \tif err != nil {\n" +
		"// \t\treturn nil, err\n" +
		"// \t}\n" +
		"// \t// map rows -> domain\n" +
		"// \treturn nil, nil\n" +
		"// }\n"
}

// GenerateModule materializes a module scaffold and updates registry wiring.
func GenerateModule(workspaceRoot string, cfg TemplateConfig, force bool) error {
	spec := cfg.Spec
	moduleDir := filepath.Join(workspaceRoot, "internal", "modules", spec.Package)
	if _, err := os.Stat(moduleDir); err == nil {
		if !force {
			return fmt.Errorf("module directory already exists: %s (re-run with force=1 to overwrite)", moduleDir)
		}
		if err := os.RemoveAll(moduleDir); err != nil {
			return fmt.Errorf("remove existing module directory: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		return fmt.Errorf("create module directory: %w", err)
	}

	files := RenderFiles(cfg)
	for name, content := range files {
		path := filepath.Join(moduleDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create directory for %s: %w", path, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}

	goModPath := filepath.Join(workspaceRoot, "go.mod")
	goMod, err := os.ReadFile(goModPath)
	if err != nil {
		return fmt.Errorf("read go.mod: %w", err)
	}
	modulePath, err := parseModulePath(string(goMod))
	if err != nil {
		return err
	}

	registryPath := filepath.Join(workspaceRoot, "internal", "modules", "modules.go")
	registryContentBytes, err := os.ReadFile(registryPath)
	if err != nil {
		return fmt.Errorf("read module registry: %w", err)
	}
	moduleImportPath := modulePath + "/internal/modules/" + spec.Package
	updated, _, err := UpdateRegistry(string(registryContentBytes), moduleImportPath, spec.Package)
	if err != nil {
		return fmt.Errorf("update module registry: %w", err)
	}
	if err := os.WriteFile(registryPath, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("write module registry: %w", err)
	}

	if cfg.Options.CreateMigration {
		if err := generateMigrationScaffold(workspaceRoot, spec, force); err != nil {
			return err
		}
	}

	return nil
}

func generateMigrationScaffold(workspaceRoot string, spec ModuleSpec, force bool) error {
	migrationsDir := filepath.Join(workspaceRoot, "db", "migrations")
	if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
		return fmt.Errorf("create migrations directory: %w", err)
	}

	existingNum := findExistingModuleMigration(migrationsDir, spec.RoutePath)
	migrationNum := existingNum
	if migrationNum == 0 {
		var err error
		migrationNum, err = nextMigrationNumber(migrationsDir)
		if err != nil {
			return err
		}
	}

	prefix := fmt.Sprintf("%06d_%s", migrationNum, spec.RoutePath)
	upPath := filepath.Join(migrationsDir, prefix+".up.sql")
	downPath := filepath.Join(migrationsDir, prefix+".down.sql")

	if !force {
		if _, err := os.Stat(upPath); err == nil {
			return fmt.Errorf("migration scaffold already exists: %s (re-run with force=1 to overwrite)", upPath)
		}
	}

	upContent := "-- Migration scaffold for module " + spec.Package + ".\n" +
		"-- Module-local sqlc files live under internal/modules/" + spec.Package + "/db/.\n" +
		"-- Add your CREATE TABLE / ALTER TABLE statements here.\n"
	downContent := "-- Rollback scaffold for module " + spec.Package + ".\n" +
		"-- Add the matching DROP / rollback statements here.\n"

	if err := os.WriteFile(upPath, []byte(upContent), 0o644); err != nil {
		return fmt.Errorf("write migration scaffold: %w", err)
	}
	if err := os.WriteFile(downPath, []byte(downContent), 0o644); err != nil {
		return fmt.Errorf("write migration rollback scaffold: %w", err)
	}
	return nil
}

func findExistingModuleMigration(migrationsDir, routePath string) int {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return 0
	}
	suffix := "_" + routePath + ".up.sql"
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, suffix) {
			continue
		}
		parts := strings.SplitN(name, "_", 2)
		if len(parts) < 2 {
			continue
		}
		n, err := strconv.Atoi(parts[0])
		if err == nil {
			return n
		}
	}
	return 0
}

func nextMigrationNumber(migrationsDir string) (int, error) {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return 0, fmt.Errorf("read migrations directory: %w", err)
	}

	maxNum := 0
	for _, entry := range entries {
		name := entry.Name()
		if len(name) < 6 {
			continue
		}
		n, err := strconv.Atoi(name[:6])
		if err != nil {
			continue
		}
		if n > maxNum {
			maxNum = n
		}
	}
	return maxNum + 1, nil
}

func toPascalCase(pkg string) string {
	parts := strings.Split(pkg, "_")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		out = append(out, strings.ToUpper(part[:1])+part[1:])
	}
	if len(out) == 0 {
		return "Module"
	}
	return strings.Join(out, "")
}

func parseModulePath(goMod string) (string, error) {
	for _, line := range strings.Split(goMod, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "module ") {
			continue
		}
		modulePath := strings.TrimSpace(strings.TrimPrefix(trimmed, "module "))
		if modulePath == "" {
			return "", errors.New("go.mod module directive is empty")
		}
		return modulePath, nil
	}
	return "", errors.New("module directive not found in go.mod")
}
