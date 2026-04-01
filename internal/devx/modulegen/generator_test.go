package modulegen

import (
	"strings"
	"testing"
)

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		name      string
		wantPkg   string
		wantRoute string
		wantErr   bool
	}{
		{name: "projects", wantPkg: "projects", wantRoute: "projects"},
		{name: "project_tasks", wantPkg: "project_tasks", wantRoute: "project-tasks"},
		{name: "project-tasks", wantPkg: "project_tasks", wantRoute: "project-tasks"},
		{name: "ProjectTasks", wantErr: true},
		{name: "123projects", wantErr: true},
		{name: "", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spec, err := NormalizeName(tc.name)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.name)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeName(%q) error = %v", tc.name, err)
			}
			if spec.Package != tc.wantPkg {
				t.Fatalf("package=%q want=%q", spec.Package, tc.wantPkg)
			}
			if spec.RoutePath != tc.wantRoute {
				t.Fatalf("route=%q want=%q", spec.RoutePath, tc.wantRoute)
			}
		})
	}
}

func TestNormalizeNameErrorMessages(t *testing.T) {
	tests := []struct {
		name        string
		wantErrPart string
	}{
		{name: "ProjectTasks", wantErrPart: "use lowercase only"},
		{name: "project!tasks", wantErrPart: "allowed characters are lowercase letters"},
		{name: "123projects", wantErrPart: "must start with a letter"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NormalizeName(tc.name)
			if err == nil {
				t.Fatalf("expected error for %q", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantErrPart) {
				t.Fatalf("error=%q missing %q", err.Error(), tc.wantErrPart)
			}
		})
	}
}

func TestUpdateRegistryAddsImportAndEntry(t *testing.T) {
	content := `package modules

import (
	"github.com/MrEthical07/superapi/internal/core/app"
	"github.com/MrEthical07/superapi/internal/modules/health"
	// MODULE_IMPORTS
)

func All() []app.Module {
	return []app.Module{
		health.New(),
		// MODULE_LIST
	}
}
`
	updated, changed, err := UpdateRegistry(content, "github.com/MrEthical07/superapi/internal/modules/projects", "projects")
	if err != nil {
		t.Fatalf("UpdateRegistry() error = %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true")
	}
	if !strings.Contains(updated, `"github.com/MrEthical07/superapi/internal/modules/projects"`) {
		t.Fatalf("missing projects import")
	}
	if !strings.Contains(updated, "\t\tprojects.New(),") {
		t.Fatalf("missing projects module entry")
	}
}

func TestUpdateRegistryIdempotentNoDuplicates(t *testing.T) {
	content := `package modules

import (
	"github.com/MrEthical07/superapi/internal/core/app"
	"github.com/MrEthical07/superapi/internal/modules/health"
	// MODULE_IMPORTS
)

func All() []app.Module {
	return []app.Module{
		health.New(),
		// MODULE_LIST
	}
}
`
	once, changed, err := UpdateRegistry(content, "github.com/MrEthical07/superapi/internal/modules/projects", "projects")
	if err != nil {
		t.Fatalf("UpdateRegistry(once) error = %v", err)
	}
	if !changed {
		t.Fatalf("expected first call to change content")
	}
	twice, changed, err := UpdateRegistry(once, "github.com/MrEthical07/superapi/internal/modules/projects", "projects")
	if err != nil {
		t.Fatalf("UpdateRegistry(twice) error = %v", err)
	}
	if changed {
		t.Fatalf("expected second call to be idempotent")
	}
	if strings.Count(twice, "projects.New(),") != 1 {
		t.Fatalf("expected exactly one projects.New() entry")
	}
	if strings.Count(twice, `/internal/modules/projects"`) != 1 {
		t.Fatalf("expected exactly one projects import")
	}
}
