package handlers

import (
	"bytes"
	"encoding/json"
	"strconv"
	"time"

	"evoting/internal/middleware"
	"evoting/internal/models"
	"evoting/internal/passkey"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

// ---------------------------------------------------------------------------
// Enrollment: "add fingerprint / face unlock to my account"
// Requires an existing logged-in session, so the student proves who they are
// with their password once, then binds a device.
// ---------------------------------------------------------------------------

// BeginPasskeyRegistration issues the creation challenge.
// POST /api/voter/passkey/register/begin
func BeginPasskeyRegistration(c *fiber.Ctx) error {
	userID := c.Locals("userID").(int)

	record, err := passkey.FindUserByID(userID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "User not found"})
	}

	user, err := passkey.NewUser(record)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Could not load existing passkeys"})
	}

	// Platform authenticator + required user verification is what turns this
	// into "fingerprint / Face ID" rather than "plug in a USB security key".
	// ResidentKey required makes it a discoverable passkey, which is what lets
	// the student sign in later without typing a matric number first.
	selection := protocol.AuthenticatorSelection{
		AuthenticatorAttachment: protocol.Platform,
		ResidentKey:             protocol.ResidentKeyRequirementRequired,
		UserVerification:        protocol.VerificationRequired,
	}

	options, session, err := passkey.Instance.BeginRegistration(
		user,
		webauthn.WithAuthenticatorSelection(selection),
		webauthn.WithExclusions(user.ExcludeList()),
		webauthn.WithConveyancePreference(protocol.PreferNoAttestation),
	)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Could not start enrollment: " + err.Error()})
	}

	handle, err := passkey.PutSession(userID, session)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Could not store challenge"})
	}

	return c.JSON(fiber.Map{
		"session_id": handle,
		"options":    options.Response, // the PublicKeyCredentialCreationOptions
	})
}

type finishRegistrationRequest struct {
	SessionID   string          `json:"session_id"`
	DeviceLabel string          `json:"device_label"`
	Credential  json.RawMessage `json:"credential"`
}

// FinishPasskeyRegistration verifies and stores the new passkey.
// POST /api/voter/passkey/register/finish
func FinishPasskeyRegistration(c *fiber.Ctx) error {
	userID := c.Locals("userID").(int)

	var req finishRegistrationRequest
	if err := c.BodyParser(&req); err != nil || len(req.Credential) == 0 {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request body"})
	}

	sessionUserID, session, ok := passkey.TakeSession(req.SessionID)
	if !ok || sessionUserID != userID {
		return c.Status(400).JSON(fiber.Map{"error": "Enrollment session expired. Please try again."})
	}

	parsed, err := protocol.ParseCredentialCreationResponseBody(bytes.NewReader(req.Credential))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Malformed authenticator response"})
	}

	record, err := passkey.FindUserByID(userID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "User not found"})
	}
	user, err := passkey.NewUser(record)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Could not load existing passkeys"})
	}

	credential, err := passkey.Instance.CreateCredential(user, session, parsed)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Verification failed: " + err.Error()})
	}

	label := req.DeviceLabel
	if label == "" {
		label = "This device"
	}
	if err := passkey.SaveCredential(userID, credential, label); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Could not save passkey"})
	}

	logAudit(userID, "PASSKEY_ENROLLED", "Biometric unlock enabled on "+label, c.IP())

	// Enrollment required user verification (fingerprint/face/PIN), so this
	// session is now biometrically verified.
	tokenStr, err := issueJWT(record, true)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to generate token"})
	}

	return c.Status(201).JSON(fiber.Map{
		"message": "Biometric unlock enabled for this device",
		"token":   tokenStr,
	})
}

// ---------------------------------------------------------------------------
// Login: "sign in with fingerprint / face"
// ---------------------------------------------------------------------------

type beginLoginRequest struct {
	MatricNumber string `json:"matric_number"`
}

// BeginPasskeyLogin issues the assertion challenge.
// POST /api/auth/passkey/login/begin
//
// If a matric number is supplied we scope the challenge to that student's
// devices. If it is omitted we run a discoverable ceremony: the browser shows
// every passkey saved for this site and the student just picks one and touches
// the sensor — no typing at all.
func BeginPasskeyLogin(c *fiber.Ctx) error {
	var req beginLoginRequest
	_ = c.BodyParser(&req)

	if req.MatricNumber == "" {
		options, session, err := passkey.Instance.BeginDiscoverableLogin(
			webauthn.WithUserVerification(protocol.VerificationRequired),
		)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Could not start sign-in"})
		}
		handle, err := passkey.PutSession(0, session)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Could not store challenge"})
		}
		return c.JSON(fiber.Map{"session_id": handle, "options": options.Response})
	}

	record, err := passkey.FindUserByMatric(req.MatricNumber)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "No biometric unlock is set up for this matric number"})
	}

	user, err := passkey.NewUser(record)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Could not load passkeys"})
	}
	if len(user.Credentials) == 0 {
		return c.Status(404).JSON(fiber.Map{"error": "No biometric unlock is set up for this matric number"})
	}

	options, session, err := passkey.Instance.BeginLogin(
		user,
		webauthn.WithUserVerification(protocol.VerificationRequired),
	)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Could not start sign-in"})
	}

	handle, err := passkey.PutSession(record.ID, session)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Could not store challenge"})
	}

	return c.JSON(fiber.Map{"session_id": handle, "options": options.Response})
}

type finishLoginRequest struct {
	SessionID  string          `json:"session_id"`
	Credential json.RawMessage `json:"credential"`
}

// FinishPasskeyLogin verifies the assertion and issues the same JWT the
// password login issues, so every downstream route works unchanged.
// POST /api/auth/passkey/login/finish
func FinishPasskeyLogin(c *fiber.Ctx) error {
	var req finishLoginRequest
	if err := c.BodyParser(&req); err != nil || len(req.Credential) == 0 {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request body"})
	}

	sessionUserID, session, ok := passkey.TakeSession(req.SessionID)
	if !ok {
		return c.Status(400).JSON(fiber.Map{"error": "Sign-in session expired. Please try again."})
	}

	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(req.Credential))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Malformed authenticator response"})
	}

	var (
		record     models.User
		credential *webauthn.Credential
	)

	if sessionUserID == 0 {
		// Discoverable login: the device tells us who it is via the user handle.
		handler := func(rawID, userHandle []byte) (webauthn.User, error) {
			id, err := passkey.DecodeUserHandle(userHandle)
			if err != nil {
				return nil, err
			}
			found, err := passkey.FindUserByID(id)
			if err != nil {
				return nil, err
			}
			record = found
			return passkey.NewUser(found)
		}

		credential, err = passkey.Instance.ValidateDiscoverableLogin(handler, session, parsed)
	} else {
		record, err = passkey.FindUserByID(sessionUserID)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "User not found"})
		}
		var user *passkey.User
		if user, err = passkey.NewUser(record); err == nil {
			credential, err = passkey.Instance.ValidateLogin(user, session, parsed)
		}
	}

	if err != nil {
		return c.Status(401).JSON(fiber.Map{"error": "Biometric verification failed"})
	}

	// A counter that moves backwards suggests the credential was cloned.
	// For an election system that is worth refusing outright.
	if credential.Authenticator.CloneWarning {
		logAudit(record.ID, "PASSKEY_CLONE_WARNING", "Signature counter regression on passkey login", c.IP())
		return c.Status(401).JSON(fiber.Map{"error": "This passkey failed a security check. Please sign in with your password."})
	}

	_ = passkey.TouchCredential(credential)

	tokenStr, err := issueJWT(record, true)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to generate token"})
	}

	logAudit(record.ID, "LOGIN_BIOMETRIC", "User logged in with biometric unlock", c.IP())

	record.PasswordHash = ""
	return c.JSON(models.LoginResponse{Token: tokenStr, User: record})
}

// ---------------------------------------------------------------------------
// Step-up: an already password-authenticated user proves presence with a
// fingerprint / face scan and receives a biometrically verified token.
// ---------------------------------------------------------------------------

// BeginBiometricVerify issues an assertion challenge for the logged-in user.
// POST /api/voter/passkey/verify/begin
func BeginBiometricVerify(c *fiber.Ctx) error {
	userID := c.Locals("userID").(int)

	record, err := passkey.FindUserByID(userID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "User not found"})
	}
	user, err := passkey.NewUser(record)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Could not load passkeys"})
	}
	if len(user.Credentials) == 0 {
		return c.Status(409).JSON(fiber.Map{
			"error": "No fingerprint or face unlock is set up yet",
			"code":  "no_passkey",
		})
	}

	options, session, err := passkey.Instance.BeginLogin(
		user,
		webauthn.WithUserVerification(protocol.VerificationRequired),
	)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Could not start verification"})
	}
	handle, err := passkey.PutSession(userID, session)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Could not store challenge"})
	}
	return c.JSON(fiber.Map{"session_id": handle, "options": options.Response})
}

// FinishBiometricVerify checks the scan and upgrades the session token.
// POST /api/voter/passkey/verify/finish
func FinishBiometricVerify(c *fiber.Ctx) error {
	userID := c.Locals("userID").(int)

	var req finishLoginRequest
	if err := c.BodyParser(&req); err != nil || len(req.Credential) == 0 {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request body"})
	}

	sessionUserID, session, ok := passkey.TakeSession(req.SessionID)
	if !ok || sessionUserID != userID {
		return c.Status(400).JSON(fiber.Map{"error": "Verification session expired. Please try again."})
	}

	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(req.Credential))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Malformed authenticator response"})
	}

	record, err := passkey.FindUserByID(userID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "User not found"})
	}
	user, err := passkey.NewUser(record)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Could not load passkeys"})
	}

	credential, err := passkey.Instance.ValidateLogin(user, session, parsed)
	if err != nil {
		return c.Status(401).JSON(fiber.Map{"error": "Biometric verification failed"})
	}
	if credential.Authenticator.CloneWarning {
		logAudit(userID, "PASSKEY_CLONE_WARNING", "Signature counter regression on step-up", c.IP())
		return c.Status(401).JSON(fiber.Map{"error": "This passkey failed a security check."})
	}
	_ = passkey.TouchCredential(credential)

	tokenStr, err := issueJWT(record, true)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to generate token"})
	}

	logAudit(userID, "BIOMETRIC_VERIFIED", "Session verified with biometric unlock", c.IP())

	record.PasswordHash = ""
	return c.JSON(models.LoginResponse{Token: tokenStr, User: record})
}

// ---------------------------------------------------------------------------
// Device management
// ---------------------------------------------------------------------------

// ListPasskeys returns the current user's enrolled devices.
// GET /api/voter/passkey
func ListPasskeys(c *fiber.Ctx) error {
	userID := c.Locals("userID").(int)

	creds, err := passkey.ListCredentials(userID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Could not load passkeys"})
	}
	return c.JSON(creds)
}

// DeletePasskey removes one enrolled device.
// DELETE /api/voter/passkey/:id
func DeletePasskey(c *fiber.Ctx) error {
	userID := c.Locals("userID").(int)

	id, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid passkey id"})
	}

	if err := passkey.DeleteCredential(userID, id); err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "Passkey not found"})
	}

	logAudit(userID, "PASSKEY_REMOVED", "Biometric unlock removed for a device", c.IP())
	return c.JSON(fiber.Map{"message": "Passkey removed"})
}

// issueJWT mirrors the token issued by the password Login handler, with the
// biometric flag set according to how the session was authenticated.
func issueJWT(user models.User, bio bool) (string, error) {
	claims := middleware.JWTClaims{
		UserID: user.ID,
		Role:   user.Role,
		Bio:    bio,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(8 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(middleware.GetJWTSecret())
}
