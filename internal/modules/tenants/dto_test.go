package tenants

import "testing"

func TestCreateTenantRequestValidate(t *testing.T) {
	tests := []struct {
		name    string
		req     createTenantRequest
		wantErr bool
	}{
		{
			name:    "valid",
			req:     createTenantRequest{Slug: "acme-co", Name: "Acme", Status: "active"},
			wantErr: false,
		},
		{
			name:    "invalid slug",
			req:     createTenantRequest{Slug: "Acme!", Name: "Acme", Status: "active"},
			wantErr: true,
		},
		{
			name:    "empty name",
			req:     createTenantRequest{Slug: "acme", Name: "", Status: "active"},
			wantErr: true,
		},
		{
			name:    "invalid status",
			req:     createTenantRequest{Slug: "acme", Name: "Acme", Status: "suspended"},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() error = %v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestParseListLimit(t *testing.T) {
	if got, err := parseListLimit(""); err != nil || got != defaultListLimit {
		t.Fatalf("default limit = (%d, %v), want (%d, nil)", got, err, defaultListLimit)
	}
	if _, err := parseListLimit("0"); err == nil {
		t.Fatalf("expected error for zero limit")
	}
	if _, err := parseListLimit("101"); err == nil {
		t.Fatalf("expected error for limit > 100")
	}
}
