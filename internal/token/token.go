// Package token issues signed impression tokens.
//
// A token binds an impression to the banner and placement it was served for,
// so the event collector can reject forged or tampered events without a
// shared database. The wire format is a versioned contract shared with
// proview-event-collector (see proview-docs, "Impression token"):
//
//	base64url(payload) "." base64url(HMAC-SHA256(key, payload)[:16])
//	payload = "1|<kid>|<impression_id>|<banner_id>|<placement>|<issued_unix>"
package token

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
)

const version = "1"

// MACSize is the number of HMAC bytes kept in the token.
const MACSize = 16

// Claims are the facts bound into a token.
type Claims struct {
	ImpressionID string
	BannerID     int64
	Placement    string
	IssuedAt     time.Time
}

// Signer creates tokens with a single active key.
type Signer struct {
	kid string
	key []byte
}

// NewSigner returns a Signer. kid must not contain '|'.
func NewSigner(kid string, key []byte) (*Signer, error) {
	if kid == "" || strings.Contains(kid, "|") {
		return nil, errors.New("token: invalid key id")
	}
	if len(key) < 32 {
		return nil, errors.New("token: key must be at least 32 bytes")
	}
	return &Signer{kid: kid, key: key}, nil
}

// Sign returns the encoded token for c.
func (s *Signer) Sign(c Claims) string {
	var b strings.Builder
	b.Grow(96)
	b.WriteString(version)
	b.WriteByte('|')
	b.WriteString(s.kid)
	b.WriteByte('|')
	b.WriteString(c.ImpressionID)
	b.WriteByte('|')
	b.WriteString(strconv.FormatInt(c.BannerID, 10))
	b.WriteByte('|')
	b.WriteString(c.Placement)
	b.WriteByte('|')
	b.WriteString(strconv.FormatInt(c.IssuedAt.Unix(), 10))
	payload := b.String()

	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(payload))
	sum := m.Sum(nil)

	enc := base64.RawURLEncoding
	return enc.EncodeToString([]byte(payload)) + "." + enc.EncodeToString(sum[:MACSize])
}
