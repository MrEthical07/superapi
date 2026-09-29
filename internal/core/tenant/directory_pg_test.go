package tenant

import (
	"errors"
	"testing"

	"github.com/MrEthical07/superapi/internal/core/db/dbtest"
)

// GetBySlug against a real Postgres (SUPERAPI_TEST_DATABASE_URL): slugs are
// stored lower-case, looked up case-insensitively, and resolve to the id.
func TestRepositoryGetBySlug(t *testing.T) {
	repo := NewRepository(dbtest.NewPostgres(t))
	ctx := t.Context()

	created, err := repo.Create(ctx, Record{ID: "t-42", Slug: "  Globex-Corp ", Name: "Globex", Status: StatusActive})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID != "t-42" || created.Slug != "globex-corp" {
		t.Fatalf("created = %+v, want id kept and slug normalized", created)
	}
	if _, err := repo.Create(ctx, Record{ID: "t-7", Slug: "dormant", Name: "Dormant", Status: StatusInactive}); err != nil {
		t.Fatalf("create inactive: %v", err)
	}

	for _, slug := range []string{"globex-corp", "GLOBEX-CORP", " Globex-Corp "} {
		got, err := repo.GetBySlug(ctx, slug)
		if err != nil {
			t.Fatalf("GetBySlug(%q): %v", slug, err)
		}
		if got.ID != "t-42" || got.Slug != "globex-corp" || !got.Active() {
			t.Fatalf("GetBySlug(%q) = %+v", slug, got)
		}
	}

	if got, err := repo.GetBySlug(ctx, "dormant"); err != nil || got.Active() {
		t.Fatalf("inactive tenant: %+v err=%v", got, err)
	}
	if _, err := repo.GetBySlug(ctx, "nobody"); !errors.Is(err, ErrTenantNotFound) {
		t.Fatalf("unknown slug: err=%v, want ErrTenantNotFound", err)
	}
	// The id is not a slug.
	if _, err := repo.GetBySlug(ctx, "t-42"); !errors.Is(err, ErrTenantNotFound) {
		t.Fatalf("id used as slug: err=%v, want ErrTenantNotFound", err)
	}
	// And the slug is not an id.
	if _, err := repo.Get(ctx, "globex-corp"); !errors.Is(err, ErrTenantNotFound) {
		t.Fatalf("slug used as id: err=%v, want ErrTenantNotFound", err)
	}
	if got, err := repo.Get(ctx, "t-42"); err != nil || got.Slug != "globex-corp" {
		t.Fatalf("Get by id: %+v err=%v", got, err)
	}

	// Slugs stay unique whatever their case.
	if _, err := repo.Create(ctx, Record{ID: "t-43", Slug: "GLOBEX-CORP", Name: "Clone", Status: StatusActive}); err == nil {
		t.Fatal("a slug differing only in case must violate uniqueness")
	}
}
