package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"

	"github.com/gin-gonic/gin"
)

const (
	// UserHashKey is the gin.Context key where the user's hashed identifier is stored.
	UserHashKey = "userHash"
)

// TokenAuth returns a Gin middleware that validates the X-Auth-Token header.
// If allowedHashes is empty, any token is accepted (open/dev mode).
// The SHA-256 hash of the token is stored in the context for downstream use.
func TokenAuth(allowedHashes []string) gin.HandlerFunc {
	allowed := make(map[string]bool, len(allowedHashes))
	for _, h := range allowedHashes {
		allowed[h] = true
	}

	return func(c *gin.Context) {
		token := c.GetHeader("X-Auth-Token")
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing X-Auth-Token header"})
			return
		}

		hash := sha256Hash(token)

		// If allowlist is configured, enforce it.
		if len(allowed) > 0 && !allowed[hash] {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}

		c.Set(UserHashKey, hash)
		c.Next()
	}
}

func sha256Hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
