package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestGenerateToken tests that the GenerateToken function returns a non-empty string of 16 characters.
func TestGenerateToken(t *testing.T) {
	token := GenerateToken()
	if len(token) != 16 {
		t.Errorf("Expected token of length 16, got %d", len(token))
	}
}

// TestLoginNewUser tests login for a new user who is not in the existing tokens.
func TestLoginNewUser(t *testing.T) {
	existingTokens := map[string]string{}
	am := NewAuthManager(existingTokens)

	// Create a login request for a new user
	body := strings.NewReader(`{"username": "newuser"}`)
	req := httptest.NewRequest(http.MethodPost, "/auth", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	// Call Login method
	am.Login(w, req)

	// Check response
	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status OK, got %v", resp.Status)
	}

	var response map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	token, ok := response["token"]
	if !ok {
		t.Fatalf("Expected token in response")
	}

	// Ensure a new token was generated
	if len(token) != 16 {
		t.Errorf("Expected a new token of length 16, got '%s'", token)
	}
}

// TestLogout tests the logout functionality.
func TestLogout(t *testing.T) {
	existingTokens := map[string]string{
		"testuser": "validtoken",
	}
	am := NewAuthManager(existingTokens)

	// Simulate user login
	am.loggedInToken["validtoken"] = "testuser"

	// Create a logout request with a valid token
	req := httptest.NewRequest(http.MethodDelete, "/auth", nil)
	req.Header.Set("Authorization", "Bearer validtoken")
	w := httptest.NewRecorder()

	// Call Logout method
	am.Logout(w, req)

	// Check response
	resp := w.Result()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("Expected status No Content, got %v", resp.Status)
	}

	// Ensure the token is invalidated
	_, valid := am.ValidateToken("validtoken")
	if valid {
		t.Errorf("Expected token to be invalidated")
	}
}

// TestValidateOldToken tests token validation for an existing token.
func TestValidateOldToken(t *testing.T) {
	existingTokens := map[string]string{
		"oldvalidtoken": "olduser",
	}
	am := NewAuthManager(existingTokens)
	am.loggedInToken["newtoken"] = "newuser"
	am.expiry["oldvalidtoken"] = time.Now().Add(24 * time.Hour)

	// Validate token
	username, valid := am.ValidateToken("oldvalidtoken")
	if !valid {
		t.Fatalf("Expected token to be valid")
	}
	if username != "olduser" {
		t.Errorf("Expected username 'testuser', got '%s'", username)
	}

	// Expire the token
	am.expiry["oldvalidtoken"] = time.Now().Add(-time.Hour)

	// Validate expired token
	_, valid = am.ValidateToken("oldvalidtoken")
	if valid {
		t.Errorf("Expected token to be expired")
	}
}

// TestOldValidateToken tests token validation for a new token.
func TestValidateNewToken(t *testing.T) {
	existingTokens := map[string]string{
		"oldvalidtoken": "olduser",
	}
	am := NewAuthManager(existingTokens)
	am.loggedInToken["newtoken"] = "newuser"
	am.expiry["newtoken"] = time.Now().Add(1 * time.Hour)

	// Validate token
	username, valid := am.ValidateToken("newtoken")
	if !valid {
		t.Fatalf("Expected token to be valid")
	}
	if username != "newuser" {
		t.Errorf("Expected username 'testuser', got '%s'", username)
	}

	// Expire the token
	am.expiry["newtoken"] = time.Now().Add(-time.Hour)

	// Validate expired token
	_, valid = am.ValidateToken("newtoken")
	if valid {
		t.Errorf("Expected token to be expired")
	}
}
