package passkey

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"evoting/internal/database"
	"evoting/internal/models"

	"github.com/go-webauthn/webauthn/webauthn"
)

// ErrNoCredentials means the student has not enrolled any passkey yet.
var ErrNoCredentials = errors.New("no passkey enrolled for this account")

// LoadCredentials returns every stored passkey for a user.
func LoadCredentials(userID int) ([]webauthn.Credential, error) {
	rows, err := database.DB.Query(`
		SELECT credential_data
		FROM webauthn_credentials
		WHERE user_id = $1
		ORDER BY created_at
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var creds []webauthn.Credential
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var c webauthn.Credential
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, err
		}
		creds = append(creds, c)
	}
	return creds, rows.Err()
}

// SaveCredential stores a freshly enrolled passkey.
func SaveCredential(userID int, cred *webauthn.Credential, label string) error {
	raw, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	if label == "" {
		label = "Unnamed device"
	}

	_, err = database.DB.Exec(`
		INSERT INTO webauthn_credentials (user_id, credential_id, credential_data, device_label, sign_count)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (credential_id) DO UPDATE
		  SET credential_data = EXCLUDED.credential_data,
		      sign_count      = EXCLUDED.sign_count
	`, userID, encodeID(cred.ID), raw, label, int64(cred.Authenticator.SignCount))

	return err
}

// TouchCredential persists the updated signature counter after a successful
// login. The counter is a clone-detection signal: if it ever goes backwards the
// library reports it via cred.Authenticator.CloneWarning.
func TouchCredential(cred *webauthn.Credential) error {
	raw, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	_, err = database.DB.Exec(`
		UPDATE webauthn_credentials
		SET credential_data = $1, sign_count = $2, last_used_at = NOW()
		WHERE credential_id = $3
	`, raw, int64(cred.Authenticator.SignCount), encodeID(cred.ID))
	return err
}

// StoredCredential is the trimmed view sent to the frontend for the
// "manage my devices" screen. The credential material itself never leaves the
// server, and no biometric data is stored anywhere in the first place.
type StoredCredential struct {
	ID          int        `json:"id"`
	DeviceLabel string     `json:"device_label"`
	CreatedAt   time.Time  `json:"created_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
}

// ListCredentials returns a user's enrolled devices for display.
func ListCredentials(userID int) ([]StoredCredential, error) {
	rows, err := database.DB.Query(`
		SELECT id, device_label, created_at, last_used_at
		FROM webauthn_credentials
		WHERE user_id = $1
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []StoredCredential{}
	for rows.Next() {
		var c StoredCredential
		var lastUsed sql.NullTime
		if err := rows.Scan(&c.ID, &c.DeviceLabel, &c.CreatedAt, &lastUsed); err != nil {
			return nil, err
		}
		if lastUsed.Valid {
			t := lastUsed.Time
			c.LastUsedAt = &t
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeleteCredential removes one enrolled device, scoped to its owner so a
// student can never delete somebody else's passkey.
func DeleteCredential(userID, credentialRowID int) error {
	res, err := database.DB.Exec(`
		DELETE FROM webauthn_credentials WHERE id = $1 AND user_id = $2
	`, credentialRowID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("passkey not found")
	}
	return nil
}

// FindUserByID loads the base user record used to build the WebAuthn user.
func FindUserByID(id int) (models.User, error) {
	var u models.User
	err := database.DB.QueryRow(`
		SELECT id, matric_number, full_name, email, department, role, is_eligible, has_voted
		FROM users WHERE id = $1
	`, id).Scan(&u.ID, &u.MatricNumber, &u.FullName, &u.Email,
		&u.Department, &u.Role, &u.IsEligible, &u.HasVoted)
	return u, err
}

// FindUserByMatric loads a user by matric number.
func FindUserByMatric(matric string) (models.User, error) {
	var u models.User
	err := database.DB.QueryRow(`
		SELECT id, matric_number, full_name, email, department, role, is_eligible, has_voted
		FROM users WHERE matric_number = $1
	`, matric).Scan(&u.ID, &u.MatricNumber, &u.FullName, &u.Email,
		&u.Department, &u.Role, &u.IsEligible, &u.HasVoted)
	return u, err
}

func encodeID(id []byte) string {
	return base64.RawURLEncoding.EncodeToString(id)
}
