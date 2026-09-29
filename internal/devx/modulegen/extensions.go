package modulegen

import "sort"

// Extension is optional scaffolding an optional feature adds to the generated
// route: a policy, cache key parts, imports and a reminder. It is switched on
// per module with its own flag (make module flags=--<flag>). A feature
// registers its extension from a file of its own in this package (named after
// the feature, so removing the feature removes the file); modulegen is a
// development tool, so registration happens in init, before any generation.
type Extension struct {
	// Flag is the command-line flag and option name (for example "org").
	Flag string
	// Help is the flag's help text.
	Help string
	// Prompt is the interactive wizard question.
	Prompt string
	// RequiresAuth makes the extension require the auth policy (the feature's
	// scope depends on the authenticated principal).
	RequiresAuth bool
	// Imports are extra import lines for routes.go.
	Imports []string
	// Notes are comment lines written above the route.
	Notes []string
	// Policies are policy lines added after AuthRequired, each already indented
	// and ending in a comma.
	Policies []string
	// CachePart is a cache.KeyPart expression added to both the cache tag spec
	// and the VaryBy of a generated cache policy. When set it replaces the
	// default per-user scoping (it must be identity-bearing).
	CachePart string
}

var extensions []Extension

// RegisterExtension adds a feature's scaffolding.
func RegisterExtension(ext Extension) {
	extensions = append(extensions, ext)
	sort.SliceStable(extensions, func(i, j int) bool { return extensions[i].Flag < extensions[j].Flag })
}

// Extensions returns the registered extensions ordered by flag.
func Extensions() []Extension {
	return append([]Extension(nil), extensions...)
}

// enabled returns the extensions switched on in opts, in flag order.
func (o TemplateOptions) enabled() []Extension {
	var out []Extension
	for _, ext := range extensions {
		if o.Extra[ext.Flag] {
			out = append(out, ext)
		}
	}
	return out
}
