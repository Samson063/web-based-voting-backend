package passkey

import (
	"log"
	"os"
	"strings"

	"github.com/go-webauthn/webauthn/webauthn"
)

// Instance is the global WebAuthn relying party.
// It is created once at startup by Initialize().
var Instance *webauthn.WebAuthn

// Initialize builds the WebAuthn relying party from environment variables.
//
// WEBAUTHN_RP_ID       the domain the passkey is bound to. NO scheme, NO port.
//
//	local dev  -> localhost
//	production -> bouestivote.vercel.app
//
// WEBAUTHN_RP_NAME     what the OS prompt shows the student ("Use your fingerprint for ___")
// WEBAUTHN_RP_ORIGINS  comma separated list of exact frontend origins (scheme + host + port)
//
//	local dev  -> http://localhost:5173
//	production -> https://bouestivote.vercel.app
//
// The RP ID must be the FRONTEND domain (where the browser runs), not the
// Render backend domain. Getting this wrong is the #1 cause of
// "The relying party ID is not a registrable domain suffix" errors.
func Initialize() {
	rpID := env("WEBAUTHN_RP_ID", "localhost")
	rpName := env("WEBAUTHN_RP_NAME", "UniVote E-Voting System")
	rawOrigins := env("WEBAUTHN_RP_ORIGINS", "http://localhost:5173")

	var origins []string
	for _, o := range strings.Split(rawOrigins, ",") {
		if o = strings.TrimSpace(o); o != "" {
			origins = append(origins, o)
		}
	}

	cfg := &webauthn.Config{
		RPDisplayName: rpName,
		RPID:          rpID,
		RPOrigins:     origins,
	}

	var err error
	if Instance, err = webauthn.New(cfg); err != nil {
		log.Fatalf("WebAuthn initialization failed: %v", err)
	}

	log.Printf("WebAuthn ready | rpID=%s | origins=%v", rpID, origins)
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
