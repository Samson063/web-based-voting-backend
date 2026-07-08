package main

import (
	"evoting/internal/database"
	"evoting/internal/handlers"
	"evoting/internal/middleware"
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
)

func main() {
	// Step 1: Connect to the database
	database.Connect()
	database.CreateTables()

	// Step 2: Create the Fiber app (our web server)
	app := fiber.New(fiber.Config{
		AppName:      "UniVote E-Voting System",
		ErrorHandler: errorHandler,
	})

	// Step 3: Attach middleware
	app.Use(logger.New())
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "Origin, Content-Type, Accept, Authorization",
		AllowMethods: "GET,POST,PUT,PATCH,DELETE",
	}))

	// Step 4: Define API routes
	api := app.Group("/api")

	// --- Public routes (no login needed) ---
	auth := api.Group("/auth")
	auth.Post("/register", handlers.Register)
	auth.Post("/login", handlers.Login)

	// Public stats for the landing page
	api.Get("/stats", handlers.GetPublicStats)

	// Public: view elections and results (transparency)
	api.Get("/elections", handlers.GetElections)
	api.Get("/elections/:election_id/candidates", handlers.GetCandidates)
	api.Get("/elections/:election_id/results", handlers.GetResults)
	api.Get("/verify/:receipt", handlers.VerifyReceipt)

	// --- Protected voter routes (must be logged in) ---
	voter := api.Group("/voter", middleware.Protected())
	voter.Get("/me", handlers.GetMe)
	voter.Post("/vote", handlers.CastVote)

	// --- Admin-only routes ---
	admin := api.Group("/admin", middleware.Protected(), middleware.AdminOnly())
	admin.Get("/stats", handlers.GetDashboardStats)
	admin.Get("/users", handlers.GetAllUsers)
	admin.Patch("/users/:id/eligibility", handlers.SetEligibility)
	admin.Post("/elections", handlers.CreateElection)
	admin.Patch("/elections/:id/toggle", handlers.ToggleElection)
	admin.Post("/candidates", handlers.AddCandidate)
	admin.Get("/audit-logs", handlers.GetAuditLogs)

	// Step 5: Start the server
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
