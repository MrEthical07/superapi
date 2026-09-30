package tenancy

import goauth "github.com/MrEthical07/goAuth"

// The core provider satisfies goauth.WebAuthnCredentialProvider and the wrapper
// inherits it by embedding; it must keep doing so, or goAuth's Build fails
// (WebAuthn enabled) or WebAuthn is lost.
var _ goauth.WebAuthnCredentialProvider = (*Provider)(nil)
