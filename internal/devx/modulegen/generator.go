package modulegen

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	registryImportsMarker = "// MODULE_IMPORTS"
	registryListMarker    = "// MODULE_LIST"
)

var validInputName = regexp.MustCompile(`^[a-z0-9_-]+$`)

type ModuleSpec struct {
	RawName   string
	Package   string
	RoutePath string
}

func NormalizeName(name string) (ModuleSpec, error) {
	input := strings.TrimSpace(name)
	raw := strings.ToLower(input)
	if raw == "" {
		return ModuleSpec{}, errors.New("module name is required")
	}
	if input != raw {
		return ModuleSpec{}, fmt.Errorf("invalid module name %q: must be lowercase", name)
	}
	if !validInputName.MatchString(raw) {
		return ModuleSpec{}, fmt.Errorf("invalid module name %q: only lowercase letters, digits, '-' and '_' are allowed", name)
	}

	parts := splitParts(raw)
	if len(parts) == 0 {
		return ModuleSpec{}, fmt.Errorf("invalid module name %q", name)
	}
	if parts[0] == "" || (parts[0][0] < 'a' || parts[0][0] > 'z') {
		return ModuleSpec{}, fmt.Errorf("invalid module name %q: must start with a letter", name)
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

func RenderFiles(spec ModuleSpec) map[string]string {
	moduleName := spec.Package
	route := spec.RoutePath
	pascalName := toPascalCase(moduleName)

	moduleFile := "package " + moduleName + "\n\n" +
		"import \"github.com/MrEthical07/superapi/internal/core/app\"\n\n" +
		"type Module struct {\n\thandler *Handler\n}\n\n" +
		"func New() *Module { return &Module{} }\n\n" +
		"var _ app.Module = (*Module)(nil)\n\n" +
		"func (m *Module) Name() string { return \"" + moduleName + "\" }\n"

	routesFile := "package " + moduleName + "\n\n" +
		"import (\n\t\"net/http\"\n\n\t\"github.com/MrEthical07/superapi/internal/core/httpx\"\n\t\"github.com/MrEthical07/superapi/internal/core/policy\"\n)\n\n" +
		"func (m *Module) Register(r httpx.Router) error {\n" +
		"\tif m.handler == nil {\n\t\tm.handler = NewHandler(NewService(NewRepo()))\n\t}\n\n" +
		"\tr.Handle(\n\t\thttp.MethodGet,\n\t\t\"/api/v1/" + route + "/ping\",\n\t\thttp.HandlerFunc(m.handler.Ping),\n\t\tpolicy.Noop(),\n\t)\n\n" +
		"\t// Example policy hooks:\n" +
		"\t// r.Handle(http.MethodGet, \"/api/v1/" + route + "\", http.HandlerFunc(m.handler.List),\n" +
		"\t// \tpolicy.AuthRequired(authProvider, authMode),\n" +
		"\t// \tpolicy.RateLimit(limiter, rule),\n" +
		"\t// \tpolicy.CacheRead(cacheMgr, cache.CacheReadConfig{TTL: 30 * time.Second}),\n" +
		"\t// )\n\n" +
		"\treturn nil\n}\n"

	dtoFile := "package " + moduleName + "\n\n" +
		"import \"github.com/MrEthical07/superapi/internal/core/errors\"\n\n" +
		"type pingResponse struct {\n\tStatus string `json:\"status\"`\n\tModule string `json:\"module\"`\n}\n\n" +
		"type create" + pascalName + "Request struct {\n\tName string `json:\"name\"`\n}\n\n" +
		"func (r create" + pascalName + "Request) Validate() error {\n\tif r.Name == \"\" {\n\t\treturn errors.New(errors.CodeBadRequest, 400, \"name is required\")\n\t}\n\treturn nil\n}\n"

	handlerFile := "package " + moduleName + "\n\n" +
		"import (\n\t\"net/http\"\n\n\t\"github.com/MrEthical07/superapi/internal/core/httpx\"\n\t\"github.com/MrEthical07/superapi/internal/core/response\"\n)\n\n" +
		"type Handler struct {\n\tsvc *Service\n}\n\n" +
		"func NewHandler(svc *Service) *Handler {\n\treturn &Handler{svc: svc}\n}\n\n" +
		"func (h *Handler) Ping(w http.ResponseWriter, r *http.Request) {\n" +
		"\tresult := h.svc.Ping()\n" +
		"\tresponse.OK(w, result, httpx.RequestIDFromContext(r.Context()))\n}\n"

	serviceFile := "package " + moduleName + "\n\n" +
		"type Service struct {\n\trepo *Repo\n}\n\n" +
		"func NewService(repo *Repo) *Service {\n\treturn &Service{repo: repo}\n}\n\n" +
		"func (s *Service) Ping() pingResponse {\n\treturn pingResponse{Status: \"ok\", Module: \"" + moduleName + "\"}\n}\n"

	repoFile := "package " + moduleName + "\n\n" +
		"type Repo struct{}\n\n" +
		"func NewRepo() *Repo {\n\treturn &Repo{}\n}\n"

	handlerTestFile := "package " + moduleName + "\n\n" +
		"import (\n\t\"encoding/json\"\n\t\"net/http\"\n\t\"net/http/httptest\"\n\t\"testing\"\n)\n\n" +
		"func TestPingHandler(t *testing.T) {\n" +
		"\th := NewHandler(NewService(NewRepo()))\n\trr := httptest.NewRecorder()\n\treq := httptest.NewRequest(http.MethodGet, \"/api/v1/" + route + "/ping\", nil)\n\n" +
		"\th.Ping(rr, req)\n\n" +
		"\tif rr.Code != http.StatusOK {\n\t\tt.Fatalf(\"status=%d want=%d\", rr.Code, http.StatusOK)\n\t}\n\n" +
		"\tvar body struct {\n\t\tOK   bool `json:\"ok\"`\n\t\tData pingResponse `json:\"data\"`\n\t}\n" +
		"\tif err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {\n\t\tt.Fatalf(\"unmarshal response: %v\", err)\n\t}\n" +
		"\tif !body.OK || body.Data.Module != \"" + moduleName + "\" || body.Data.Status != \"ok\" {\n" +
		"\t\tt.Fatalf(\"unexpected response: %+v\", body)\n\t}\n}\n"

	serviceTestFile := "package " + moduleName + "\n\n" +
		"import \"testing\"\n\n" +
		"func TestServicePing(t *testing.T) {\n" +
		"\ts := NewService(NewRepo())\n\tres := s.Ping()\n\tif res.Status != \"ok\" {\n\t\tt.Fatalf(\"status=%q want=%q\", res.Status, \"ok\")\n\t}\n\n" +
		"\tif res.Module != \"" + moduleName + "\" {\n\t\tt.Fatalf(\"module=%q want=%q\", res.Module, \"" + moduleName + "\")\n\t}\n}\n"

	return map[string]string{
		"module.go":       moduleFile,
		"routes.go":       routesFile,
		"dto.go":          dtoFile,
		"handler.go":      handlerFile,
		"service.go":      serviceFile,
		"repo.go":         repoFile,
		"handler_test.go": handlerTestFile,
		"service_test.go": serviceTestFile,
	}
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

func GenerateModule(workspaceRoot string, spec ModuleSpec, force bool) error {
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

	for name, content := range RenderFiles(spec) {
		path := filepath.Join(moduleDir, name)
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

	return nil
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
