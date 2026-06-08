package auth

// START HERE:
//
// This file defines the default role registry used by goAuth.
//
// Projects should customize roles here instead of modifying
// goauth_provider.go.
//
// Notes:
//
//   - Roles are authentication identities.
//   - Permissions determine authorization.
//   - Every role registered here must exist in the role map below.
//   - Account.DefaultRole must reference a valid role.
//
// Common customizations:
//
//   - Add project-specific roles
//   - Add permission mappings
//   - Change default role behavior
//
// Examples:
//
//   user
//   moderator
//   admin
//
//   customer
//   seller
//   support
//
//   member
//   staff
//   owner
//
// See also:
//   config.go
//   goauth_provider.go

// ------------------------------------------------------------
// Permissions
// ------------------------------------------------------------

// PermissionWhoAmI is a minimal permission required by the
// default template auth route.
//
// Projects will typically replace this registry with their own
// permission set.
const (
	PermissionWhoAmI = "system.whoami"
)

// ------------------------------------------------------------
// Roles
// ------------------------------------------------------------

const (
	RoleUser  = "user"
	RoleAdmin = "admin"
)

// ------------------------------------------------------------
// Default Registry
// ------------------------------------------------------------

// DefaultPermissions contains all permissions registered with
// goAuth during startup.
//
// Builder.Build() requires at least one permission.
var DefaultPermissions = []string{
	PermissionWhoAmI,
}

// DefaultRoles maps roles to permissions.
//
// Projects should customize this registry to match their
// authorization model.
var DefaultRoles = map[string][]string{
	RoleUser: {
		PermissionWhoAmI,
	},
	RoleAdmin: {
		PermissionWhoAmI,
	},
}
