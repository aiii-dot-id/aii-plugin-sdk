module github.com/aiii-dot-id/aii-plugin-sdk

go 1.25

// The one third-party dependency: cloudflare/circl provides the two
// post-quantum signature schemes (ML-DSA-87, SLH-DSA-SHA2-256s) the
// signing chain uses. The guest library (pkg/aiiosdk) does not import it.
require github.com/cloudflare/circl v1.6.3

require (
	golang.org/x/crypto v0.30.0 // indirect
	golang.org/x/sys v0.28.0 // indirect
)
