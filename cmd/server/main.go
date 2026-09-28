package main

import (
	"evoting/internal/database"
	"evoting/internal/handlers"
	"evoting/internal/middleware"
	"evoting/internal/passkey"
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/joho/godotenv"
)

func main() {
	// Load .env file locally — on Render, env vars are set in the dashboard
	err := godotenv.Load()
	if err != nil {
		log.Println("⚠️  No .env file — using system environment variables")
	}

	database.Connect()
	database.CreateTables()

	// Fingerprint / Face unlock (WebAuthn). Must run after the env is loaded.
	passkey.Initialize()

	app := fiber.New(fiber.Config{
		AppName:      "UniVote E-Voting System",
		ErrorHandler: errorHandler,
	})

	app.Use(logger.New())

	// Allow requests from your deployed Vercel frontend
	app.Use(cors.New(cors.Config{
		AllowOrigins: "https://bouestivote.vercel.app, http://localhost:3000, http://localhost:5173",
		AllowHeaders: "Origin, Content-Type, Accept, Authorization",
		AllowMethods: "GET,POST,PUT,PATCH,DELETE",
	}))

	api := app.Group("/api")

	// --- Public routes ---
	auth := api.Group("/auth")
	auth.Post("/register", handlers.Register)
	auth.Post("/login", handlers.Login)

	// Biometric sign-in (fingerprint / Face ID) — public, the passkey itself
	// is the credential being proven.
	auth.Post("/passkey/login/begin", handlers.BeginPasskeyLogin)
	auth.Post("/passkey/login/finish", handlers.FinishPasskeyLogin)

	api.Get("/stats", handlers.GetPublicStats)
	api.Get("/elections", handlers.GetElections)
	api.Get("/elections/:election_id/candidates", handlers.GetCandidates)
	api.Get("/elections/:election_id/results", handlers.GetResults)
	api.Get("/verify/:receipt", handlers.VerifyReceipt)

	// --- Protected voter routes ---
	voter := api.Group("/voter", middleware.Protected())
	voter.Get("/me", handlers.GetMe)
	voter.Post("/vote", handlers.CastVote)

	// Enrolling and managing biometric unlock requires an existing session.
	voter.Get("/passkey", handlers.ListPasskeys)
	voter.Delete("/passkey/:id", handlers.DeletePasskey)
	voter.Post("/passkey/register/begin", handlers.BeginPasskeyRegistration)
	voter.Post("/passkey/register/finish", handlers.FinishPasskeyRegistration)

	// --- Admin-only routes ---
	admin := api.Group("/admin", middleware.Protected(), middleware.AdminOnly())
	admin.Get("/stats", handlers.GetDashboardStats)
	admin.Get("/users", handlers.GetAllUsers)
	admin.Patch("/users/:id/eligibility", handlers.SetEligibility)
	admin.Post("/elections", handlers.CreateElection)
	admin.Patch("/elections/:id/toggle", handlers.ToggleElection)
	admin.Post("/candidates", handlers.AddCandidate)
	admin.Get("/audit-logs", handlers.GetAuditLogs)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("🚀 Server running on http://localhost:%s", port)
	log.Fatal(app.Listen(":" + port))
}

func errorHandler(c *fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	if e, ok := err.(*fiber.Error); ok {
		code = e.Code
	}
	return c.Status(code).JSON(fiber.Map{"error": err.Error()})
}
