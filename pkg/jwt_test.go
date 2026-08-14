package pkg

import (
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestMain(m *testing.M) {
	os.Setenv("JWT_SECRET", "test-secret-key-for-jwt-tests")
	code := m.Run()
	os.Unsetenv("JWT_SECRET")
	os.Exit(code)
}

func TestGenerateJWT(t *testing.T) {
	token, err := GenerateJWT(42, "testuser")
	if err != nil {
		t.Fatalf("GenerateJWT() error = %v", err)
	}
	if token == "" {
		t.Error("GenerateJWT() returned empty token")
	}
}

func TestGenerateJWT_NoSecret(t *testing.T) {
	os.Unsetenv("JWT_SECRET")
	defer os.Setenv("JWT_SECRET", "test-secret-key-for-jwt-tests")

	_, err := GenerateJWT(1, "user")
	if err == nil {
		t.Error("GenerateJWT() should error when JWT_SECRET is not set")
	}
}

func TestValidateJWT_RoundTrip(t *testing.T) {
	token, err := GenerateJWT(99, "roundtrip")
	if err != nil {
		t.Fatalf("GenerateJWT() error = %v", err)
	}

	claims, err := ValidateJWT(token)
	if err != nil {
		t.Fatalf("ValidateJWT() error = %v", err)
	}

	if claims.UserID != 99 {
		t.Errorf("ValidateJWT() UserID = %d, want 99", claims.UserID)
	}
	if claims.Username != "roundtrip" {
		t.Errorf("ValidateJWT() Username = %q, want %q", claims.Username, "roundtrip")
	}
	if claims.Issuer != "moongate-api" {
		t.Errorf("ValidateJWT() Issuer = %q, want %q", claims.Issuer, "moongate-api")
	}
}

func TestValidateJWT_InvalidToken(t *testing.T) {
	_, err := ValidateJWT("invalid.token.string")
	if err == nil {
		t.Error("ValidateJWT() should error for invalid token")
	}
}

func TestValidateJWT_EmptyToken(t *testing.T) {
	_, err := ValidateJWT("")
	if err == nil {
		t.Error("ValidateJWT() should error for empty token")
	}
}

func TestValidateJWT_WrongSecret(t *testing.T) {
	claims := Claims{
		UserID:   1,
		Username: "hacker",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			Issuer:    "moongate-api",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte("different-secret-key"))
	if err != nil {
		t.Fatalf("Failed to sign token: %v", err)
	}

	_, err = ValidateJWT(signed)
	if err == nil {
		t.Error("ValidateJWT() should fail when token was signed with a different secret")
	}
}

func TestValidateJWT_ExpiredToken(t *testing.T) {
	expiredClaims := Claims{
		UserID:   1,
		Username: "expired",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			Issuer:    "moongate-api",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, expiredClaims)
	signed, err := token.SignedString([]byte("test-secret-key-for-jwt-tests"))
	if err != nil {
		t.Fatalf("Failed to sign token: %v", err)
	}

	_, err = ValidateJWT(signed)
	if err == nil {
		t.Error("ValidateJWT() should fail for expired token")
	}
}

func TestValidateJWT_MissingSecret(t *testing.T) {
	os.Unsetenv("JWT_SECRET")
	defer os.Setenv("JWT_SECRET", "test-secret-key-for-jwt-tests")

	_, err := ValidateJWT("some.token.here")
	if err == nil {
		t.Error("ValidateJWT() should error when JWT_SECRET is not set")
	}
}