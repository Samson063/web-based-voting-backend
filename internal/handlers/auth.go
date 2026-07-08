package handlers

import (
	"evoting/internal/database"
	"evoting/internal/middleware"
	"evoting/internal/models"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// Register creates a new student account
func Register(c *fiber.Ctx) error {
	var req models.RegisterRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request body"})
	}

	// Basic validation
	if req.MatricNumber == "" || req.FullName == "" || req.Email == "" || req.Password == "" {
		return c.Status(400).JSON(fiber.Map{"error": "All fields are required"})
	}

	// Hash the password — NEVER store plain text passwords
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to process password"})
	}

	// Insert the new user
	var userID int
	err = database.DB.QueryRow(`
		INSERT INTO users (matric_number, full_name, email, department, password_hash, role)
		VALUES ($1, $2, $3, $4, $5, 'voter')
		RETURNING id
	`, req.MatricNumber, req.FullName, req.Email, req.Department, string(hash)).Scan(&userID)

	if err != nil {
		// Check for duplicate matric/email (PostgreSQL unique violation code 23505)
		if err.Error() != "" {
			return c.Status(409).JSON(fiber.Map{"error": "Matric number or email already registered"})
		}
		return c.Status(500).JSON(fiber.Map{"error": "Failed to create account"})
	}

	// Log the action
	logAudit(userID, "REGISTER", "New voter account created for "+req.MatricNumber, c.IP())

	return c.Status(201).JSON(fiber.Map{
		"message": "Account created successfully. You can now log in.",
	})
}

// Login authenticates a user and returns a JWT token
func Login(c *fiber.Ctx) error {
	var req models.LoginRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request body"})
	}

	// Find the user by matric number
	var user models.User
	err := database.DB.QueryRow(`
		SELECT id, matric_number, full_name, email, department, password_hash, role, is_eligible, has_voted
		FROM users WHERE matric_number = $1
	`, req.MatricNumber).Scan(
		&user.ID, &user.MatricNumber, &user.FullName, &user.Email,
		&user.Department, &user.PasswordHash, &user.Role, &user.IsEligible, &user.HasVoted,
	)

	if err != nil {
		// Don't reveal whether the user exists (security best practice)
		return c.Status(401).JSON(fiber.Map{"error": "Invalid matric number or password"})
	}

	// Check the password against the stored hash
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return c.Status(401).JSON(fiber.Map{"error": "Invalid matric number or password"})
	}

	// Create a JWT token valid for 8 hours
	claims := middleware.JWTClaims{
		UserID: user.ID,
		Role:   user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(8 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString(middleware.GetJWTSecret())
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to generate token"})
	}

	logAudit(user.ID, "LOGIN", "User logged in", c.IP())

	// Clear password hash before sending
	user.PasswordHash = ""

	return c.JSON(models.LoginResponse{
		Token: tokenStr,
		User:  user,
	})
}

// GetMe returns the current logged-in user's profile
func GetMe(c *fiber.Ctx) error {
	userID := c.Locals("userID").(int)

	var user models.User
	err := database.DB.QueryRow(`
		SELECT id, matric_number, full_name, email, department, role, is_eligible, has_voted, created_at
		FROM users WHERE id = $1
	`, userID).Scan(
		&user.ID, &user.MatricNumber, &user.FullName, &user.Email,
		&user.Department, &user.Role, &user.IsEligible, &user.HasVoted, &user.CreatedAt,
	)

	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "User not found"})
	}

	return c.JSON(user)
}
