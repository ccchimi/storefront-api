// Package auth handles password hashing and session tokens.
package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// DefaultIterations follows the OWASP recommendation for PBKDF2-HMAC-SHA256.
const DefaultIterations = 600_000

const (
	scheme  = "pbkdf2_sha256"
	saltLen = 16
	keyLen  = 32
)

// Hasher hashes passwords with PBKDF2. The iteration count is stored inside
// every hash, so raising it later doesn't lock out existing users.
type Hasher struct {
	Iterations int
}

// Hash returns an encoded hash like "pbkdf2_sha256$600000$<salt>$<key>".
func (h Hasher) Hash(password string) (string, error) {
	iter := h.Iterations
	if iter <= 0 {
		iter = DefaultIterations
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, iter, keyLen)
	if err != nil {
		return "", err
	}
	enc := base64.RawStdEncoding
	return fmt.Sprintf("%s$%d$%s$%s", scheme, iter, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// Verify reports whether password matches an encoded hash.
func Verify(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != scheme {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter <= 0 {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := enc.DecodeString(parts[3])
	if err != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// NewToken returns a random bearer token and the hash to store for it.
// Only the hash goes into the database, so a leaked database dump doesn't
// hand out live sessions.
func NewToken() (token, hash string) {
	token = rand.Text()
	return token, HashToken(token)
}

// HashToken is the stored form of a bearer token.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
