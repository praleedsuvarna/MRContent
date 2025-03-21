package routes

import (
	"github.com/gofiber/fiber/v2"
	"github.com/praleedsuvarna/shared-libs/middleware"
)

func SetupRoutes(app *fiber.App) {
	// Add a debug endpoint to test token parsing
	app.Get("/debug-token", middleware.AuthDebugger(), func(c *fiber.Ctx) error {
		// This will just respond with the token info from the header
		return c.JSON(fiber.Map{
			"message":     "Token debug info logged to console",
			"auth_header": c.Get("Authorization"),
		})
	})
	MRContentRoutes(app)
}
