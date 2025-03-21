// main.go
package main

import (
	// "UserManagement/config"
	// "UserManagement/routes"
	"MRContent/controllers"
	"MRContent/routes"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gofiber/fiber/v2"
	"github.com/praleedsuvarna/shared-libs/config"
)

func main() {
	config.LoadEnv()
	config.ConnectDB()

	// Initialize NATS connection for media processing
	nc, err := controllers.InitNATS()
	if err != nil {
		log.Printf("Warning: Failed to initialize NATS connection: %v", err)
		// Continue without NATS as it's not critical for the API to function
	} else {
		defer controllers.CloseNATS()
	}

	// Set up Fiber app
	app := fiber.New(fiber.Config{
		ErrorHandler: customErrorHandler,
	})

	// Setup routes
	routes.SetupRoutes(app)

	// Initialize callback handlers for media processing results
	err = controllers.InitCallbackHandlers(app, nc)
	if err != nil {
		log.Printf("Warning: Failed to initialize callback handlers: %v", err)
	}

	// Start the server in a goroutine
	go func() {
		port := os.Getenv("PORT")
		if port == "" {
			port = "3000"
		}

		log.Printf("Starting server on port %s", port)
		if err := app.Listen(":" + port); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Set up graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")
	if err := app.Shutdown(); err != nil {
		log.Fatalf("Error during server shutdown: %v", err)
	}

	log.Println("Server successfully shutdown")
}

// Custom error handler for better error responses
func customErrorHandler(c *fiber.Ctx, err error) error {
	// Default 500 status code
	code := fiber.StatusInternalServerError

	// Check if it's a Fiber error
	if e, ok := err.(*fiber.Error); ok {
		code = e.Code
	}

	// Return JSON response with error message
	return c.Status(code).JSON(fiber.Map{
		"error": err.Error(),
	})
}
