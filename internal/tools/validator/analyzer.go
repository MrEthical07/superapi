package validator

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	corepolicy "github.com/MrEthical07/superapi/internal/core/policy"
)

// Diagnostic reports one static route-policy validation issue.
type Diagnostic struct {
	// File is source file containing the issue.
	File string `json:"file"`
	// Line is source line for the route declaration.
	Line int `json:"line"`
	// Message describes violated validation rule.
	Message string `json:"message"`
}

// AnalyzePaths scans Go files and validates route policy wiring statically.
func AnalyzePaths(paths []string) ([]Diagnostic, error) {
	if len(paths) == 0 {
		paths = []string{"./..."}
	}

	files, err := collectGoFiles(paths)
	if err != nil {
		return nil, err
	}

	diagnostics := make([]Diagnostic, 0, 16)
	for _, filePath := range files {
		fileDiagnostics, fileErr := analyzeFile(filePath)
		if fileErr != nil {
			return nil, fileErr
		}
		diagnostics = append(diagnostics, fileDiagnostics...)
	}

	sort.SliceStable(diagnostics, func(i, j int) bool {
		if diagnostics[i].File != diagnostics[j].File {
			return diagnostics[i].File < diagnostics[j].File
		}
		return diagnostics[i].Line < diagnostics[j].Line
	})

	return diagnostics, nil
}

func collectGoFiles(paths []string) ([]string, error) {
	seen := make(map[string]struct{}, 64)
	files := make([]string, 0, 64)

	for _, raw := range paths {
		target := strings.TrimSpace(raw)
		if target == "" {
			continue
		}

		if strings.HasSuffix(target, "/...") || strings.HasSuffix(target, "\\...") {
			root := strings.TrimSuffix(strings.TrimSuffix(target, "/..."), "\\...")
			if root == "" {
				root = "."
			}
			if err := walkGoFiles(root, &files, seen); err != nil {
				return nil, err
			}
			continue
		}

		info, err := os.Stat(target)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", target, err)
		}
		if info.IsDir() {
			if err := walkGoFiles(target, &files, seen); err != nil {
				return nil, err
			}
			continue
		}
		if strings.HasSuffix(strings.ToLower(info.Name()), ".go") {
			abs, absErr := filepath.Abs(target)
			if absErr != nil {
				return nil, absErr
			}
			if _, exists := seen[abs]; !exists {
				seen[abs] = struct{}{}
				files = append(files, abs)
			}
		}
	}

	sort.Strings(files)
	return files, nil
}

func walkGoFiles(root string, files *[]string, seen map[string]struct{}) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "vendor" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".go") {
			return nil
		}
		if strings.HasSuffix(strings.ToLower(d.Name()), "_test.go") {
			return nil
		}

		abs, absErr := filepath.Abs(path)
		if absErr != nil {
			return absErr
		}
		if _, exists := seen[abs]; exists {
			return nil
		}
		seen[abs] = struct{}{}
		*files = append(*files, abs)
		return nil
	})
}

func analyzeFile(filePath string) ([]Diagnostic, error) {
	fset := token.NewFileSet()
	fileNode, err := parser.ParseFile(fset, filePath, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", filePath, err)
	}

	diagnostics := make([]Diagnostic, 0, 8)
	ast.Inspect(fileNode, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Handle" {
			return true
		}

		line := fset.Position(call.Lparen).Line
		if call.Ellipsis.IsValid() {
			diagnostics = append(diagnostics, Diagnostic{
				File:    filePath,
				Line:    line,
				Message: "variadic spread policies are not supported by static verify; pass policies directly",
			})
			return true
		}

		if len(call.Args) < 3 {
			diagnostics = append(diagnostics, Diagnostic{File: filePath, Line: line, Message: "route Handle call must include method, pattern, and handler"})
			return true
		}

		if len(call.Args) == 3 {
			// No policy chain to validate. Skip to avoid false positives when method/pattern are dynamic.
			return true
		}

		method, methodErr := extractMethod(call.Args[0])
		if methodErr != nil {
			diagnostics = append(diagnostics, Diagnostic{File: filePath, Line: line, Message: methodErr.Error()})
			return true
		}
		pattern, patternErr := extractStringLiteral(call.Args[1])
		if patternErr != nil {
			diagnostics = append(diagnostics, Diagnostic{File: filePath, Line: line, Message: patternErr.Error()})
			return true
		}

		metas := make([]corepolicy.Metadata, 0, len(call.Args)-3)
		policyParseFailed := false
		for _, arg := range call.Args[3:] {
			meta, parseErr := parsePolicyMetadata(arg)
			if parseErr != nil {
				policyParseFailed = true
				diagnostics = append(diagnostics, Diagnostic{File: filePath, Line: line, Message: parseErr.Error()})
				continue
			}
			metas = append(metas, meta)
		}
		if policyParseFailed {
			return true
		}

		if err := corepolicy.ValidateRouteMetadata(method, pattern, metas); err != nil {
			diagnostics = append(diagnostics, Diagnostic{
				File:    filePath,
				Line:    line,
				Message: err.Error(),
			})
		}

		return true
	})

	return diagnostics, nil
}

func extractMethod(expr ast.Expr) (string, error) {
	switch v := expr.(type) {
	case *ast.SelectorExpr:
		switch v.Sel.Name {
		case "MethodGet":
			return "GET", nil
		case "MethodHead":
			return "HEAD", nil
		case "MethodPost":
			return "POST", nil
		case "MethodPut":
			return "PUT", nil
		case "MethodPatch":
			return "PATCH", nil
		case "MethodDelete":
			return "DELETE", nil
		case "MethodOptions":
			return "OPTIONS", nil
		default:
			return "", fmt.Errorf("unsupported http method expression: %s", v.Sel.Name)
		}
	case *ast.BasicLit:
		value, err := extractStringLiteral(v)
		if err != nil {
			return "", fmt.Errorf("unsupported method literal: %v", err)
		}
		return strings.ToUpper(strings.TrimSpace(value)), nil
	default:
		return "", fmt.Errorf("unsupported method expression type %T", expr)
	}
}

func extractStringLiteral(expr ast.Expr) (string, error) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", fmt.Errorf("expected string literal")
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", err
	}
	return value, nil
}

func parsePolicyMetadata(expr ast.Expr) (corepolicy.Metadata, error) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return corepolicy.Metadata{}, fmt.Errorf("policy expression must be a call")
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return corepolicy.Metadata{}, fmt.Errorf("policy call must use selector syntax")
	}

	name := selector.Sel.Name
	meta := corepolicy.Metadata{Name: name}
	switch name {
	case "AuthRequired":
		meta.Type = corepolicy.PolicyTypeAuthRequired
	case "RequirePerm":
		meta.Type = corepolicy.PolicyTypeRequirePerm
	case "RequireAnyPerm":
		meta.Type = corepolicy.PolicyTypeRequireAnyPerm
	case "TenantRequired":
		meta.Type = corepolicy.PolicyTypeTenantRequired
	case "TenantMatchFromPath":
		meta.Type = corepolicy.PolicyTypeTenantMatchFromPath
		if len(call.Args) > 0 {
			param, err := extractStringLiteral(call.Args[0])
			if err == nil {
				meta.TenantPathParam = param
			}
		}
	case "RateLimit", "RateLimitWithKeyer":
		meta.Type = corepolicy.PolicyTypeRateLimit
	case "CacheRead":
		meta.Type = corepolicy.PolicyTypeCacheRead
		meta.CacheRead = parseCacheReadMetadata(call)
	case "CacheInvalidate":
		meta.Type = corepolicy.PolicyTypeCacheInvalidate
		meta.CacheInvalidate = parseCacheInvalidateMetadata(call)
	case "RequireJSON":
		meta.Type = corepolicy.PolicyTypeRequireJSON
	case "WithHeader":
		meta.Type = corepolicy.PolicyTypeWithHeader
	case "CacheControl":
		meta.Type = corepolicy.PolicyTypeCacheControl
	case "Noop":
		meta.Type = corepolicy.PolicyTypeNoop
	default:
		return corepolicy.Metadata{}, fmt.Errorf("unsupported policy constructor %s", name)
	}

	return meta, nil
}

func parseCacheReadMetadata(call *ast.CallExpr) corepolicy.CacheReadMetadata {
	meta := corepolicy.CacheReadMetadata{}
	if call == nil || len(call.Args) < 2 {
		return meta
	}
	cfgLit, ok := call.Args[1].(*ast.CompositeLit)
	if !ok {
		return meta
	}

	for _, elt := range cfgLit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		keyName := keyExprName(kv.Key)
		switch keyName {
		case "AllowAuthenticated":
			if value, ok := boolLiteral(kv.Value); ok {
				meta.AllowAuthenticated = value
			}
		case "VaryBy":
			varyBy, ok := kv.Value.(*ast.CompositeLit)
			if !ok {
				continue
			}
			for _, varyElt := range varyBy.Elts {
				varyKV, ok := varyElt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				varyKey := keyExprName(varyKV.Key)
				flag, flagOK := boolLiteral(varyKV.Value)
				if !flagOK {
					continue
				}
				switch varyKey {
				case "UserID":
					meta.VaryByUserID = flag
				case "TenantID":
					meta.VaryByTenantID = flag
				}
			}
		}
	}
	return meta
}

func parseCacheInvalidateMetadata(call *ast.CallExpr) corepolicy.CacheInvalidateMetadata {
	meta := corepolicy.CacheInvalidateMetadata{}
	if call == nil || len(call.Args) < 2 {
		return meta
	}
	cfgLit, ok := call.Args[1].(*ast.CompositeLit)
	if !ok {
		return meta
	}
	for _, elt := range cfgLit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if keyExprName(kv.Key) != "TagSpecs" {
			continue
		}
		tagsLit, ok := kv.Value.(*ast.CompositeLit)
		if !ok {
			continue
		}
		meta.TagSpecCount = len(tagsLit.Elts)
	}
	return meta
}

func keyExprName(expr ast.Expr) string {
	switch v := expr.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return v.Sel.Name
	default:
		return ""
	}
}

func boolLiteral(expr ast.Expr) (bool, bool) {
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return false, false
	}
	switch ident.Name {
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		return false, false
	}
}
