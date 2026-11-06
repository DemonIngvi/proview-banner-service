package token

import (
	"strings"
	"testing"
	"time"
)

// goldenKey and goldenToken form the cross-service contract vector. The same
// pair is asserted in proview-event-collector; changing the format breaks it.
const (
	goldenKey   = "0123456789abcdef0123456789abcdef"
	goldenToken = "MXxrMXxpbXAtMXw0Mnxob21lX3RvcHwxNzk0MDAwMDAw.oFXqVFugR3h7xtVG1mFroA"
)

func TestSignGolden(t *testing.T) {
	s, err := NewSigner("k1", []byte(goldenKey))
	if err != nil {
		t.Fatal(err)
	}
	got := s.Sign(Claims{ImpressionID: "imp-1", BannerID: 42, Placement: "home_top", IssuedAt: time.Unix(1794000000, 0)})
	if got != goldenToken {
		t.Fatalf("token = %s, want %s", got, goldenToken)
	}
}

func TestSignDiffersByKey(t *testing.T) {
	c := Claims{ImpressionID: "a", BannerID: 1, Placement: "p", IssuedAt: time.Unix(1, 0)}
	s1, _ := NewSigner("k1", []byte(strings.Repeat("a", 32)))
	s2, _ := NewSigner("k1", []byte(strings.Repeat("b", 32)))
	if s1.Sign(c) == s2.Sign(c) {
		t.Fatal("different keys produced the same token")
	}
}

func TestNewSignerValidation(t *testing.T) {
	if _, err := NewSigner("", []byte(goldenKey)); err == nil {
		t.Error("empty kid accepted")
	}
	if _, err := NewSigner("a|b", []byte(goldenKey)); err == nil {
		t.Error("kid with separator accepted")
	}
	if _, err := NewSigner("k", []byte("short")); err == nil {
		t.Error("short key accepted")
	}
}
