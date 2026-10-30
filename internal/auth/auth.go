package auth

import (
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"github.com/AtmosferaSoltec/atm-storage-api/internal/platform/httpserver"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"strings"
)

type Claims struct {
	jwt.RegisteredClaims
	Role   string `json:"role"`
	Tenant string `json:"tenant"`
	System string `json:"system"`
}
type Verifier struct {
	key    *rsa.PublicKey
	issuer string
}

func New(publicKeyBase64, issuer string) (*Verifier, error) {
	data, err := base64.StdEncoding.DecodeString(publicKeyBase64)
	if err != nil {
		return nil, errors.New("invalid AUTH_PUBLIC_KEY_BASE64")
	}
	key, err := jwt.ParseRSAPublicKeyFromPEM(data)
	if err != nil || key.N.BitLen() < 2048 {
		return nil, errors.New("invalid RSA public key (minimum 2048 bits)")
	}
	return &Verifier{key: key, issuer: issuer}, nil
}
func (v *Verifier) Verify(raw string) (Claims, error) {
	var claims Claims
	_, err := jwt.ParseWithClaims(raw, &claims, func(t *jwt.Token) (any, error) { return v.key, nil }, jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer(v.issuer), jwt.WithExpirationRequired(), jwt.WithIssuedAt())
	if err != nil || claims.Subject == "" || claims.IssuedAt == nil || (claims.Role != "superadmin" && claims.Role != "user" && claims.Role != "service") {
		return Claims{}, errors.New("invalid access token")
	}
	return claims, nil
}
func (v *Verifier) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		parts := strings.Fields(c.GetHeader("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			httpserver.Error(c, 401, "unauthorized", "Missing or invalid access token.")
			return
		}
		claims, err := v.Verify(parts[1])
		if err != nil {
			httpserver.Error(c, 401, "unauthorized", "Missing or invalid access token.")
			return
		}
		c.Set("auth_claims", claims)
		c.Next()
	}
}
func FromContext(c *gin.Context) Claims {
	value, _ := c.Get("auth_claims")
	claims, _ := value.(Claims)
	return claims
}
