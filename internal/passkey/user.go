package passkey

import (
	"encoding/binary"
	"errors"

	"evoting/internal/models"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// User adapts the project's existing models.User to the webauthn.User interface
// the library requires. It carries the credentials already enrolled by that
// student so the library can check assertions against them.
type User struct {
	Record      models.User
	Credentials []webauthn.Credential
}

// NewUser loads a user's stored passkeys and wraps them for the library.
func NewUser(record models.User) (*User, error) {
	creds, err := LoadCredentials(record.ID)
	if err != nil {
		return nil, err
	}
	return &User{Record: record, Credentials: creds}, nil
}

// WebAuthnID is the stable, opaque user handle stored inside the passkey on the
// student's device. We encode the numeric users.id as 8 big-endian bytes so it
// can be decoded again during a discoverable ("just touch the sensor") login.
func (u *User) WebAuthnID() []byte {
	return EncodeUserHandle(u.Record.ID)
}

// WebAuthnName is the account identifier shown in the OS passkey picker.
func (u *User) WebAuthnName() string {
	return u.Record.MatricNumber
}

// WebAuthnDisplayName is the friendly name shown in the OS prompt.
func (u *User) WebAuthnDisplayName() string {
	if u.Record.FullName != "" {
		return u.Record.FullName
	}
	return u.Record.MatricNumber
}

// WebAuthnCredentials returns every passkey this student has enrolled.
func (u *User) WebAuthnCredentials() []webauthn.Credential {
	return u.Credentials
}

// ExcludeList stops a student from enrolling the same device twice.
func (u *User) ExcludeList() []protocol.CredentialDescriptor {
	out := make([]protocol.CredentialDescriptor, 0, len(u.Credentials))
	for _, c := range u.Credentials {
		out = append(out, c.Descriptor())
	}
	return out
}

// EncodeUserHandle turns a users.id into the 8-byte handle stored on the device.
func EncodeUserHandle(id int) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(id))
	return b
}

// DecodeUserHandle reverses EncodeUserHandle.
func DecodeUserHandle(handle []byte) (int, error) {
	if len(handle) != 8 {
		return 0, errors.New("unexpected user handle length")
	}
	return int(binary.BigEndian.Uint64(handle)), nil
}
