package auth

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"
)

// AuthManager manages user authentication and token storage
type AuthManager struct {
	existingTokens map[string]string    // username -> token that is already logged in
	loggedInToken  map[string]string    // token -> username that is now logged in
	expiry         map[string]time.Time // token -> expiration time
	mtx            sync.Mutex           // to protect token access
}

func NewAuthManager(existingTokens map[string]string) *AuthManager {
	return &AuthManager{
		existingTokens: existingTokens,
		loggedInToken:  make(map[string]string),
		expiry:         make(map[string]time.Time),
	}
}

// Set expiration time for existing tokens to be 24 hours
func (am *AuthManager) SetExistingTokens() {
	for token := range am.existingTokens {
		am.expiry[token] = time.Now().Add(24 * time.Hour)
		// am.expiry[token] = time.Now().Add(10 * time.Second)
	}
}

// GenerateToken generates a random token
func GenerateToken() string {
	letters := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_.?!~"
	token := make([]byte, 16)
	for i := range token {
		token[i] = letters[rand.Intn(len(letters))]
	}
	return string(token)
}

// HandlePreflight handles the CORS preflight requests
func (am *AuthManager) HandlePreflight(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, DELETE")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.WriteHeader(http.StatusOK)
}

// Login handles login requests and generates a token
func (am *AuthManager) Login(w http.ResponseWriter, r *http.Request) {
	// Set CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")

	var req struct {
		Username string `json:"username"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	token := GenerateToken()

	// Store token and set expiration (1 hour)
	am.mtx.Lock()
	am.loggedInToken[token] = req.Username
	am.expiry[token] = time.Now().Add(time.Hour)
	am.mtx.Unlock()

	resp := map[string]string{
		"token": token,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Logout handles token invalidation
func (am *AuthManager) Logout(w http.ResponseWriter, r *http.Request) {
	// Set CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	token := r.Header.Get("Authorization")
	slog.Info("token: ", "token", token)
	if len(token) > 7 && token[:7] == "Bearer " {
		token = token[7:]
	} else {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	am.mtx.Lock()
	slog.Info("loggedInToken: ", "loggedInToken", am.loggedInToken)
	slog.Info("token: ", "token", token)
	_, tokenExistsNew := am.loggedInToken[token]
	_, tokenExistsOld := am.existingTokens[token]
	if tokenExistsNew {
		// If token exists and is new, delete it and return 204 No Content
		delete(am.loggedInToken, token)
		delete(am.expiry, token)
		am.mtx.Unlock()
		w.WriteHeader(http.StatusNoContent)
	} else if tokenExistsOld {
		// If token exists and is old, not delete it and return 204 No Content
		am.mtx.Unlock()
		w.WriteHeader(http.StatusNoContent)
	} else {
		// If token does not exist, return 401 Unauthorized
		am.mtx.Unlock()
		w.WriteHeader(http.StatusUnauthorized)
		resp := map[string]string{"error": "unauthorized"}
		json.NewEncoder(w).Encode(resp)
	}
}

// ValidateToken checks if the token is valid and not expired
func (am *AuthManager) ValidateToken(token string) (string, bool) {
	am.mtx.Lock()
	defer am.mtx.Unlock()

	slog.Info("token: ", "token", token)
	slog.Info("loggedInToken: ", "loggedInToken", am.loggedInToken)
	slog.Info("existingTokens: ", "existingTokens", am.existingTokens)
	usernameNew, existsNew := am.loggedInToken[token]
	usernameOld, existsOld := am.existingTokens[token]
	slog.Info("existsNew: ", "existsNew", existsNew)
	slog.Info("existsOld: ", "existsOld", existsOld)
	// Check if token exists in either loggedInToken or existingTokens
	if !existsNew && !existsOld {
		slog.Info("token not exists")
		return "", false
	}

	// Get the username from the token
	var username string
	if existsNew {
		username = usernameNew
	}
	if existsOld {
		username = usernameOld
	}
	// Check if token has expired
	if time.Now().After(am.expiry[token]) {
		delete(am.loggedInToken, token)
		delete(am.expiry, token)
		slog.Info("token expired")
		return "", false
	}

	return username, true
}

// GetUsernameFromRequest extracts the username from the request's authentication token
func (am *AuthManager) GetUsernameFromRequest(r *http.Request) (string, error) {
	// Extract the token from the Authorization header
	token := r.Header.Get("Authorization")
	if len(token) > 7 && strings.HasPrefix(token, "Bearer ") {
		token = token[7:]
	} else {
		return "", fmt.Errorf("no token provided or invalid token format")
	}

	// Validate the token and get the username
	username, valid := am.ValidateToken(token)
	if !valid {
		return "", fmt.Errorf("invalid or expired token")
	}

	return username, nil
}
