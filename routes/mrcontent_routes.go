package routes

import (
	"MRContent/controllers"

	"github.com/gofiber/fiber/v2"
	"github.com/praleedsuvarna/shared-libs/middleware"
)

func MRContentRoutes(app *fiber.App) {
	// Group routes with prefix and authentication middleware
	mrContent := app.Group("/mr-content", middleware.AuthMiddleware)

	// CRUD operations
	mrContent.Post("/", controllers.CreateMRContent)      // Create new MR content
	mrContent.Get("/:id", controllers.GetMRContent)       // Get single MR content
	mrContent.Put("/:id", controllers.UpdateMRContent)    // Update MR content
	mrContent.Delete("/:id", controllers.DeleteMRContent) // Soft delete MR content
	mrContent.Get("/", controllers.ListMRContents)        // List all MR contents with pagination
}
