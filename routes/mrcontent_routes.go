package routes

import (
	"MRContent/controllers"
	"fmt"

	"github.com/gofiber/fiber/v2"
	"github.com/praleedsuvarna/shared-libs/middleware"
)

func MRContentRoutes(app *fiber.App) {
	// Public route for ref_id (must be registered BEFORE the id route to avoid conflicts)
	app.Get("/mr-content/ref/:ref_id", controllers.GetMRContentByRefID)

	mrContent := app.Group("/mr-content", middleware.AuthMiddleware)

	// CRUD operations requiring authentication
	mrContent.Post("/", controllers.CreateMRContent)      // Create new MR content
	mrContent.Get("/:id", controllers.GetMRContent)       // Get single MR content by ID
	mrContent.Put("/:id", controllers.UpdateMRContent)    // Update MR content
	mrContent.Delete("/:id", controllers.DeleteMRContent) // Soft delete MR content
	mrContent.Get("/", controllers.ListMRContents)        // List all MR contents with pagination
}

// Debug middleware to diagnose the issue
func DebugMiddleware(c *fiber.Ctx) error {
	fmt.Println("==== Request Debug Info ====")
	fmt.Printf("Path: %s\n", c.Path())
	fmt.Printf("Method: %s\n", c.Method())
	fmt.Printf("Headers: %v\n", c.GetReqHeaders())
	fmt.Println("===========================")
	return c.Next()
}
