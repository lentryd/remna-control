// Package middleware holds Fiber middleware: admin session auth and request
// logging.
package middleware

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"

	"remna-control/internal/apperr"
	"remna-control/internal/audit"
)

const SessionCookie = "remnactl_session"

// SessionTTL is how long an admin stays logged in.
const SessionTTL = 7 * 24 * time.Hour

// LocalsAdmin is the fiber.Ctx.Locals key holding the admin's username.
const LocalsAdmin = "admin"

// IssueSession signs a session token for username.
func IssueSession(secret, username string) (string, error) {
	claims := jwt.RegisteredClaims{
		Subject:   username,
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(SessionTTL)),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

// VerifySession returns the username of a valid session token.
func VerifySession(secret, token string) (string, error) {
	var claims jwt.RegisteredClaims
	_, err := jwt.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		return "", err
	}
	return claims.Subject, nil
}

// RequireSession rejects requests without a valid session cookie and puts
// the admin's name into Locals and the request context (for audit logs).
func RequireSession(secret string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		token := c.Cookies(SessionCookie)
		if token == "" {
			return apperr.Status(fiber.StatusUnauthorized, "auth.required", "not signed in")
		}
		username, err := VerifySession(secret, token)
		if err != nil {
			return apperr.Status(fiber.StatusUnauthorized, "auth.expired", "session expired")
		}
		c.Locals(LocalsAdmin, username)
		c.SetUserContext(audit.WithActor(c.UserContext(), username))
		return c.Next()
	}
}
