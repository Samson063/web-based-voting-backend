package middleware

import (
	"os"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

// JWTClaims holds the data stored in the token
type JWTClaims struct {
	UserID int    `json:"user_id"`
	Role   string `json:"role"`
	// Bio is true only when this session passed a fingerprint / face check.
	// Password login issues Bio=false; passkey login or the step-up
	// verification endpoint issues Bio=true.
	Bio bool `json:"bio"`
	jwt.RegisteredClaims
}

// GetJWTSecret returns the secret key used to sign tokens
func GetJWTSecret() []byte {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "evoting-super-secret-change-in-production"
	}
	return []byte(secret)
}

// Protected is middleware that checks for a valid JWT on protected routes
func Protected() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Get the Authorization header: "Bearer <token>"
		authHeader := c.Get("Authorization")
		if authHeader == "" {
			return c.Status(401).JSON(fiber.Map{"error": "Missing authorization header"})
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			return c.Status(401).JSON(fiber.Map{"error": "Invalid authorization format"})
		}

		tokenStr := parts[1]

		// Parse and validate the token
		token, err := jwt.ParseWithClaims(tokenStr, &JWTClaims{}, func(t *jwt.Token) (interface{}, error) {
			return GetJWTSecret(), nil
		})

		if err != nil || !token.Valid {
			return c.Status(401).JSON(fiber.Map{"error": "Invalid or expired token"})
		}

		claims, ok := token.Claims.(*JWTClaims)
		if !ok {
			return c.Status(401).JSON(fiber.Map{"error": "Invalid token claims"})
		}

		// Store user info in context so handlers can use it
		c.Locals("userID", claims.UserID)
		c.Locals("role", claims.Role)
		c.Locals("bio", claims.Bio)

		return c.Next()
	}
}

// AdminOnly only allows admin users through
func AdminOnly() fiber.Handler {
	return func(c *fiber.Ctx) error {
		role, ok := c.Locals("role").(string)
		if !ok || role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "Admin access required"})
		}
		return c.Next()
	}
}

// BiometricRequired blocks the request unless this session has passed a
// fingerprint / face check. Admins are exempt so a broken sensor can never lock
// the election out of its own administration.
//
// Emergency switch: set BIOMETRIC_REQUIRED=false in the environment to disable
// enforcement without redeploying code.
func BiometricRequired() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if strings.EqualFold(os.Getenv("BIOMETRIC_REQUIRED"), "false") {
			return c.Next()
		}
		if role, _ := c.Locals("role").(string); role == "admin" {
			return c.Next()
		}
		if bio, _ := c.Locals("bio").(bool); !bio {
			return c.Status(403).JSON(fiber.Map{
				"error": "Fingerprint or face verification is required",
				"code":  "biometric_required",
			})
		}
		return c.Next()
	}
}
