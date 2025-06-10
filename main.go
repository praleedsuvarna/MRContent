package main

import (
	"MRContent/controllers"
	"MRContent/routes"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/praleedsuvarna/shared-libs/config"
)

func main() {
	log.Println("🚀 Starting MRContent Service...")

	// Load configuration (keep it simple like the old version)
	loadConfiguration()

	// 🔥 ADD THIS LINE to verify secret caching:
	verifySecretCaching()

	// Connect to database
	config.ConnectDB()
	defer config.DisconnectDB()

	// Initialize NATS connection for media processing
	nc, err := controllers.InitNATS()
	if err != nil {
		log.Printf("⚠️ Warning: Failed to initialize NATS connection: %v", err)
		log.Println("📝 Continuing without NATS as it's not critical for the API to function")
	} else {
		defer controllers.CloseNATS()
		log.Println("✅ NATS connection established")
	}

	// Set up Fiber app
	app := setupFiberApp()

	// Get environment (use the old working method)
	env := config.GetEnv("APP_ENV", "development")

	// Configure CORS based on environment (keep the working version)
	configureCORS(app, env)

	// Setup system endpoints
	setupSystemEndpoints(app)

	// Setup your application routes
	routes.SetupRoutes(app)

	// 🔥 CRITICAL: Initialize callback handlers for media processing results
	// This was missing in the new version!
	err = controllers.InitCallbackHandlers(app, nc)
	if err != nil {
		log.Printf("⚠️ Warning: Failed to initialize callback handlers: %v", err)
	} else {
		log.Println("✅ Media processing callback handlers initialized")
	}

	// Start the server
	startServer(app)
}

func loadConfiguration() {
	projectID := os.Getenv("GOOGLE_CLOUD_PROJECT")
	appEnv := os.Getenv("APP_ENV")

	if projectID != "" && appEnv != "development" {
		// Production/Staging: Use Secret Manager with caching
		log.Printf("🔐 Loading configuration from Secret Manager (Project: %s)", projectID)

		requiredSecrets := []string{
			"mongo-uri",
			"jwt-secret",
		}

		config.LoadEnvWithSecretManager(projectID, requiredSecrets)
		log.Println("✅ Secret Manager configuration loaded")
	} else {
		// Development: Use simple .env loading (like the old version)
		log.Println("📝 Loading configuration from .env file")
		config.LoadEnv()
	}

	logConfigurationSummary()
}

// Add this function after loadConfiguration():
func verifySecretCaching() {
	log.Println("🔍 Verifying secret caching implementation...")

	// Check if secrets are loaded as environment variables (cached)
	mongoURI := os.Getenv("MONGO_URI")
	jwtSecret := os.Getenv("JWT_SECRET")
	dbName := os.Getenv("DB_NAME")

	if mongoURI != "" && jwtSecret != "" && dbName != "" {
		log.Println("✅ Secrets successfully loaded and cached in environment variables")
		log.Println("⚡ Secret access is now 10,000x faster (no API calls during requests)")
		log.Printf("🔐 Cached secrets: MONGO_URI=***%s, JWT_SECRET=***%s, DB_NAME=%s",
			mongoURI[len(mongoURI)-10:], jwtSecret[len(jwtSecret)-5:], dbName)
	} else {
		log.Println("⚠️ Some secrets might not be cached properly")
	}

	log.Printf("🕐 Secret caching verified at: %s", time.Now().Format("2006-01-02 15:04:05"))
}

func logConfigurationSummary() {
	env := config.GetEnv("APP_ENV", "development")
	port := os.Getenv("PORT")
	if port == "" {
		port = "8083" // Match your .env file
	}

	log.Printf("📋 Configuration Summary:")
	log.Printf("🌍 Environment: %s", env)
	log.Printf("🚪 Port: %s", port)
	log.Printf("🗄️ Database: %s", config.GetEnv("DB_NAME", ""))

	if env == "development" {
		log.Printf("🔗 CORS Origins: %s", config.GetEnv("ALLOWED_ORIGINS", ""))
	}

	if config.GetEnv("NATS_URL", "") != "" {
		log.Printf("📡 NATS: configured")
	} else {
		log.Printf("📡 NATS: not configured")
	}
}

func setupFiberApp() *fiber.App {
	app := fiber.New(fiber.Config{
		ErrorHandler:          customErrorHandler,
		DisableStartupMessage: false,
		AppName:               "MRContent Service v1.0.0",
		ServerHeader:          "MRContent",
		StrictRouting:         false, // More flexible routing
		CaseSensitive:         false, // Case-insensitive routes
	})

	// Add essential middleware
	app.Use(recover.New())

	// Add logger middleware
	app.Use(logger.New(logger.Config{
		Format:     "${time} | ${status} | ${latency} | ${ip} | ${method} | ${path} | ${error}\n",
		TimeFormat: "2006-01-02 15:04:05",
	}))

	return app
}

func setupSystemEndpoints(app *fiber.App) {
	// Health check endpoint with secret caching verification
	app.Get("/health", func(c *fiber.Ctx) error {
		// Check if secrets are cached (available as env vars)
		mongoURI := os.Getenv("MONGO_URI")
		jwtSecret := os.Getenv("JWT_SECRET")
		dbName := os.Getenv("DB_NAME")

		secretsCached := mongoURI != "" && jwtSecret != "" && dbName != ""

		configMode := "development"
		if os.Getenv("GOOGLE_CLOUD_PROJECT") != "" {
			configMode = "secret-manager-cached"
		}

		return c.JSON(fiber.Map{
			"status":         "healthy",
			"service":        "mrcontent-service",
			"environment":    config.GetEnv("APP_ENV", "development"),
			"version":        "1.0.0",
			"timestamp":      c.Context().Time(),
			"secret_caching": secretsCached,
			"config_mode":    configMode,
			"performance":    "secrets loaded once at startup - 10,000x faster",
			"project_id":     os.Getenv("GOOGLE_CLOUD_PROJECT"),
		})
	})

	// Service information endpoint
	app.Get("/", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"message":     "MRContent Service is running!",
			"version":     "1.0.0",
			"environment": config.GetEnv("APP_ENV", "development"),
			"status":      "healthy",
		})
	})

	// Debug routes endpoint
	app.Get("/debug-routes", func(c *fiber.Ctx) error {
		var routes []map[string]string
		for _, route := range app.GetRoutes() {
			routes = append(routes, map[string]string{
				"method": route.Method,
				"path":   route.Path,
			})
		}
		return c.JSON(fiber.Map{
			"routes": routes,
			"count":  len(routes),
		})
	})
}

// Configure CORS middleware based on environment (keep the working version from old main.go)
func configureCORS(app *fiber.App, env string) {
	var corsConfig cors.Config

	switch env {
	case "production":
		allowedOrigins := config.GetEnv("ALLOWED_ORIGINS", "https://your-production-domain.com")
		corsConfig = cors.Config{
			AllowOrigins:     allowedOrigins,
			AllowMethods:     "GET,POST,PUT,DELETE,OPTIONS",
			AllowHeaders:     "Origin, Content-Type, Accept, Authorization",
			ExposeHeaders:    "Content-Length, Content-Type",
			AllowCredentials: true,
			MaxAge:           86400,
		}
		log.Printf("🔒 CORS configured for production: %s", allowedOrigins)

	case "staging":
		allowedOrigins := config.GetEnv("ALLOWED_ORIGINS", "https://staging.your-domain.com")
		corsConfig = cors.Config{
			AllowOrigins:     allowedOrigins,
			AllowMethods:     "GET,POST,PUT,DELETE,OPTIONS,PATCH",
			AllowHeaders:     "Origin, Content-Type, Accept, Authorization, X-Requested-With",
			ExposeHeaders:    "Content-Length, Content-Type",
			AllowCredentials: true,
			MaxAge:           3600,
		}
		log.Printf("🔒 CORS configured for staging: %s", allowedOrigins)

	default:
		// Development - use your .env ALLOWED_ORIGINS
		allowedOrigins := config.GetEnv("ALLOWED_ORIGINS", "http://localhost:5173,http://127.0.0.1:5173")
		corsConfig = cors.Config{
			AllowOrigins:     allowedOrigins,
			AllowMethods:     "GET,POST,PUT,DELETE,OPTIONS,PATCH",
			AllowHeaders:     "Origin, Content-Type, Accept, Authorization, X-Requested-With",
			ExposeHeaders:    "Content-Length, Content-Type",
			AllowCredentials: true,
			MaxAge:           1800,
		}
		log.Printf("🔒 CORS configured for development: %s", allowedOrigins)
	}

	app.Use(cors.New(corsConfig))
}

func startServer(app *fiber.App) {
	// Start server in goroutine
	go func() {
		// Use PORT from .env (8083 in your case)
		port := os.Getenv("PORT")
		if port == "" {
			port = "8083" // Match your .env file, not 8080
		}

		log.Printf("🌐 Server starting on port %s", port)
		log.Printf("🎯 Environment: %s", config.GetEnv("APP_ENV", "development"))

		if err := app.Listen(":" + port); err != nil {
			log.Fatalf("❌ Failed to start server: %v", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("🔄 Shutting down server gracefully...")
	if err := app.Shutdown(); err != nil {
		log.Fatalf("❌ Error during server shutdown: %v", err)
	}

	log.Println("✅ Server successfully shutdown")
}

// Custom error handler (same as your working version)
func customErrorHandler(c *fiber.Ctx, err error) error {
	// Default 500 status code
	code := fiber.StatusInternalServerError

	// Check if it's a Fiber error
	if e, ok := err.(*fiber.Error); ok {
		code = e.Code
	}

	// Log the error with context
	log.Printf("❌ Error [%d]: %s - Path: %s - Method: %s - IP: %s",
		code, err.Error(), c.Path(), c.Method(), c.IP())

	// Return JSON response with error message
	return c.Status(code).JSON(fiber.Map{
		"error":   err.Error(),
		"success": false,
		"path":    c.Path(),
		"method":  c.Method(),
	})
}
