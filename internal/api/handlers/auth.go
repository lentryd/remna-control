package handlers

import (
	"crypto/subtle"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"

	"remna-control/internal/api/middleware"
	"remna-control/internal/apperr"
	"remna-control/internal/remnawave"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Login checks the credentials against the panel (POST /api/auth/login) or
// the local fallback admin, and sets the session cookie.
func (h *Handlers) Login(c *fiber.Ctx) error {
	var req loginRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	if req.Username == "" || req.Password == "" {
		return apperr.New("auth.credentials_required", "enter username and password")
	}

	ok := h.Config.AdminPassword != "" &&
		subtle.ConstantTimeCompare([]byte(req.Username), []byte(h.Config.AdminUsername)) == 1 &&
		subtle.ConstantTimeCompare([]byte(req.Password), []byte(h.Config.AdminPassword)) == 1
	if !ok {
		err := h.RW.Login(c.UserContext(), req.Username, req.Password)
		if err != nil {
			slog.Info("login failed", "username", req.Username, "error", err)
			if apiErr, isAPI := err.(*remnawave.APIError); isAPI && apiErr.Status < 500 {
				return apperr.Status(fiber.StatusUnauthorized, "auth.invalid", "wrong username or password")
			}
			return apperr.Wrap(err, "panel.unavailable", "panel unavailable: {{error}}").WithStatus(fiber.StatusBadGateway)
		}
	}

	token, err := middleware.IssueSession(h.Config.JWTSecret, req.Username)
	if err != nil {
		return err
	}
	c.Cookie(h.sessionCookie(token, time.Now().Add(middleware.SessionTTL)))
	return c.JSON(fiber.Map{"username": req.Username})
}

func (h *Handlers) Logout(c *fiber.Ctx) error {
	c.Cookie(h.sessionCookie("", time.Unix(0, 0)))
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handlers) Me(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"username": c.Locals(middleware.LocalsAdmin)})
}

func (h *Handlers) sessionCookie(value string, expires time.Time) *fiber.Cookie {
	return &fiber.Cookie{
		Name:     middleware.SessionCookie,
		Value:    value,
		Path:     "/",
		Expires:  expires,
		HTTPOnly: true,
		Secure:   h.Config.SecureCookie,
		SameSite: fiber.CookieSameSiteLaxMode,
	}
}
