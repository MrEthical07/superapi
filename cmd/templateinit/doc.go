// Command templateinit turns a fresh clone of the SuperAPI template into your
// own project, once, right after "Use this template".
//
//	make init module=github.com/acme/foo name="Foo API"
//	go run ./cmd/templateinit --module github.com/acme/foo --name "Foo API" --no-tenancy --dry-run
//
// It rewrites the Go module path everywhere, resets template-owned metadata
// (README title, CHANGELOG, LICENSE holder, SECURITY contact, issue links),
// strips template-maintainer-only content, optionally prunes whole features,
// and finally deletes itself (unless --keep-init).
//
// Pruning is driven by explicit markers in the tree. A line containing
// "template:begin <feature>" starts a block that ends at the matching
// "template:end <feature>" line; the block is removed when the feature is
// pruned, and only the marker lines are removed when it is kept. Whole files
// and directories per feature are listed in features.go.
//
// Every run is idempotent and --dry-run prints the plan without writing.
package main
