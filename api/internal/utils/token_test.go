package utils

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cbellee/photo-api/internal/models"
	jwtLib "github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Test helpers ────────────────────────────────────────────────────

var testHMACSecret = []byte("test-secret-key-at-least-32-bytes!")

// signTestJWT creates a signed JWT with the given roles and expiry.
func signTestJWT(t *testing.T, roles []string, expiry time.Time) string {
	t.Helper()
	claims := models.MyClaims{
		Roles: roles,
		RegisteredClaims: jwtLib.RegisteredClaims{
			ExpiresAt: jwtLib.NewNumericDate(expiry),
			IssuedAt:  jwtLib.NewNumericDate(time.Now()),
		},
	}
	token := jwtLib.NewWithClaims(jwtLib.SigningMethodHS256, claims)
	signed, err := token.SignedString(testHMACSecret)
	require.NoError(t, err)
	return signed
}

// testKeyfunc returns a jwt.Keyfunc that always uses the test HMAC key.
func testKeyfunc(token *jwtLib.Token) (interface{}, error) {
	if _, ok := token.Method.(*jwtLib.SigningMethodHMAC); !ok {
		return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
	}
	return testHMACSecret, nil
}

// ── extractToken tests ──────────────────────────────────────────────

func TestExtractToken_NoAuthHeader(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	_, err := extractToken(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no access token")
}

func TestExtractToken_ValidBearerToken(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer my-token-value")
	tok, err := extractToken(req)
	require.NoError(t, err)
	assert.Equal(t, "my-token-value", tok)
}

func TestExtractToken_EmptyAuthHeader(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "")
	_, err := extractToken(req)
	assert.Error(t, err)
}

// ── VerifyToken tests ───────────────────────────────────────────────

func TestVerifyToken_NoAuthHeader(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	claims, err := VerifyToken(req, "", testKeyfunc)
	assert.Error(t, err)
	assert.Nil(t, claims)
	assert.Contains(t, err.Error(), "no access token")
}

func TestVerifyToken_InvalidToken(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer not-a-valid-jwt")
	claims, err := VerifyToken(req, "", testKeyfunc)
	assert.Error(t, err)
	assert.Nil(t, claims)
	assert.Contains(t, err.Error(), "parsing JWT")
}

func TestVerifyToken_ExpiredToken(t *testing.T) {
	token := signTestJWT(t, []string{"admin"}, time.Now().Add(-time.Hour))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	claims, err := VerifyToken(req, "", testKeyfunc)
	assert.Error(t, err)
	assert.Nil(t, claims)
	assert.Contains(t, err.Error(), "parsing JWT")
}

func TestVerifyToken_ValidToken(t *testing.T) {
	token := signTestJWT(t, []string{"photo.upload", "admin"}, time.Now().Add(time.Hour))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	claims, err := VerifyToken(req, "", testKeyfunc)
	require.NoError(t, err)
	require.NotNil(t, claims)
	assert.Equal(t, []string{"photo.upload", "admin"}, claims.Roles)
}

func TestVerifyToken_ValidTokenSingleRole(t *testing.T) {
	token := signTestJWT(t, []string{"reader"}, time.Now().Add(30*time.Minute))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	claims, err := VerifyToken(req, "", testKeyfunc)
	require.NoError(t, err)
	require.NotNil(t, claims)
	assert.Equal(t, []string{"reader"}, claims.Roles)
}
