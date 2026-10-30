package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"github.com/golang-jwt/jwt/v5"
	"testing"
	"time"
)

func TestVerifierRejectsInvalidTokens(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	v, err := New(base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), "atm-auth-dev")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, issuer, role, subject string
		expires                     time.Time
		valid                       bool
	}{
		{"valid", "atm-auth-dev", "superadmin", "admin", time.Now().Add(time.Minute), true},
		{"expired", "atm-auth-dev", "user", "user", time.Now().Add(-time.Minute), false},
		{"wrong environment", "atm-auth", "superadmin", "admin", time.Now().Add(time.Minute), false},
		{"unknown role", "atm-auth-dev", "admin", "admin", time.Now().Add(time.Minute), false},
		{"no subject", "atm-auth-dev", "superadmin", "", time.Now().Add(time.Minute), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: tc.subject, Issuer: tc.issuer, IssuedAt: jwt.NewNumericDate(time.Now()), ExpiresAt: jwt.NewNumericDate(tc.expires)}, Role: tc.role}
			raw, err := jwt.NewWithClaims(jwt.SigningMethodRS256, c).SignedString(key)
			if err != nil {
				t.Fatal(err)
			}
			_, err = v.Verify(raw)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "admin", "role": "superadmin", "iss": "atm-auth-dev", "exp": time.Now().Add(time.Hour).Unix()}).SignedString(der)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = v.Verify(raw); err == nil {
		t.Fatal("accepted algorithm confusion")
	}
	raw, err = jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"sub": "admin", "role": "superadmin", "iss": "atm-auth-dev", "iat": time.Now().Unix()}).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = v.Verify(raw); err == nil {
		t.Fatal("accepted missing expiration")
	}
}
