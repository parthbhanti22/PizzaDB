package server

import (
	"sync"
)

// AuthManager provides thread-safe token-based authentication.
// It is designed to support dynamic token registration at runtime
// (e.g., from a cloud dashboard control-plane) without requiring
// a process restart.
type AuthManager struct {
	mu     sync.RWMutex
	tokens map[string]bool
}

// NewAuthManager creates an AuthManager pre-loaded with the given tokens.
// Pass an empty slice for a manager with no initial tokens.
func NewAuthManager(tokens []string) *AuthManager {
	m := &AuthManager{
		tokens: make(map[string]bool, len(tokens)),
	}
	for _, t := range tokens {
		if t != "" {
			m.tokens[t] = true
		}
	}
	return m
}

// ValidateToken checks whether a token is authorized.
// Uses a read-lock so multiple goroutines can validate concurrently
// without blocking each other — critical for high-throughput socket handling.
func (am *AuthManager) ValidateToken(token string) bool {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.tokens[token]
}

// AddToken registers a new API token at runtime.
// Thread-safe: acquires an exclusive write-lock.
// This method is the hook for dynamic key sync from the cloud dashboard.
func (am *AuthManager) AddToken(token string) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.tokens[token] = true
}

// RemoveToken revokes an API token at runtime.
// Thread-safe: acquires an exclusive write-lock.
func (am *AuthManager) RemoveToken(token string) {
	am.mu.Lock()
	defer am.mu.Unlock()
	delete(am.tokens, token)
}

// TokenCount returns the number of registered tokens.
// Useful for diagnostics and health checks.
func (am *AuthManager) TokenCount() int {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return len(am.tokens)
}
