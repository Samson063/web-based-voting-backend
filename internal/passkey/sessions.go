package passkey

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

// Every WebAuthn ceremony is two HTTP calls: "begin" hands the browser a
// one-time challenge, "finish" sends back the signed response. The challenge
// must be remembered server-side in between — a client that could choose its
// own challenge could replay an old signature.
//
// This keeps them in memory with a short TTL. That is fine for a single server
// process (Render runs one instance). If you ever scale to multiple instances
// behind a load balancer, move this into Postgres or Redis, otherwise "begin"
// and "finish" can land on different machines and logins will fail.

const sessionTTL = 5 * time.Minute

type ceremony struct {
	data    webauthn.SessionData
	userID  int // 0 for discoverable login, where the user isn't known yet
	expires time.Time
}

var (
	mu       sync.Mutex
	sessions = make(map[string]ceremony)
	once     sync.Once
)

// PutSession stores challenge data and returns the opaque handle the client
// must send back with the "finish" request.
func PutSession(userID int, data *webauthn.SessionData) (string, error) {
	once.Do(startJanitor)

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	handle := base64.RawURLEncoding.EncodeToString(raw)

	mu.Lock()
	sessions[handle] = ceremony{data: *data, userID: userID, expires: time.Now().Add(sessionTTL)}
	mu.Unlock()

	return handle, nil
}

// TakeSession retrieves and immediately consumes a stored challenge. Each
// challenge is usable exactly once.
func TakeSession(handle string) (int, webauthn.SessionData, bool) {
	mu.Lock()
	defer mu.Unlock()

	c, ok := sessions[handle]
	if !ok {
		return 0, webauthn.SessionData{}, false
	}
	delete(sessions, handle)

	if time.Now().After(c.expires) {
		return 0, webauthn.SessionData{}, false
	}
	return c.userID, c.data, true
}

func startJanitor() {
	go func() {
		for range time.Tick(time.Minute) {
			now := time.Now()
			mu.Lock()
			for k, v := range sessions {
				if now.After(v.expires) {
					delete(sessions, k)
				}
			}
			mu.Unlock()
		}
	}()
}
