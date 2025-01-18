package main

import (
	"PiliPili_Backend/config" // Import config package
	"PiliPili_Backend/logger"
	"PiliPili_Backend/middleware" // Import middleware package
	"PiliPili_Backend/streamer"   // Import streamer package
	"fmt"
	"github.com/gin-gonic/gin"
	"log"
	"os"
)

// initializeConfig initializes the configuration from the config file.
func initializeConfig(configFile string) error {
	logger.Info("Initializing config...")

	err := config.Initialize(configFile, "")
	if err != nil {
		log.Printf("Error initializing config: %v", err)
		return err
	}

	logger.Info("Configuration initialized successfully")

	// LogLevel is now part of the config package
	loglevel := config.GetConfig().LogLevel
	logger.InitializeLogger(loglevel)

	// Initialize the Signature instance
	encipher := config.GetConfig().Encipher
	if err := streamer.InitializeSignature(encipher); err != nil {
		logger.Error("Failed to initialize Signature", "error", err)
		return err
	}
	logger.Info("Signature initialized successfully")

	return nil
}

// initializeGinEngine initializes the Gin engine with the necessary middlewares and routes.
func initializeGinEngine() *gin.Engine {
	logger.Info("Initializing Gin engine...")

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	r.Use(middleware.CorsMiddleware())
	r.GET("/stream", streamer.Remote)

	logger.Info("Gin engine initialized successfully")
	return r
}

// startServer starts the Gin server on the configured port.
func startServer(r *gin.Engine) error {
	logger.Info("Starting the server...")

	port := config.GetConfig().Port
	if port == "" {
		port = "60002"
	}
	err := r.Run("0.0.0.0:" + port)
	if err != nil {
		logger.Error("Error starting server: %v", err)
		return err
	}

	logger.Info("Server started successfully on port", port)
	return nil
}

// handleRequest processes the entire request handling flow.
func handleRequest(configFile string) error {
	logger.SetDefaultLogger()
	logger.Info("\n-----------------------------------------------\n")
	logger.Info("Start request handle.")

	if err := initializeConfig(configFile); err != nil {
		return err
	}
	r := initializeGinEngine()
	if err := startServer(r); err != nil {
		return err
	}

	logger.Info("Request handling completed successfully.")
	logger.Info("\n-----------------------------------------------\n")
	return nil
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Println("Please provide the configuration file as an argument.")
		return
	}
	configFile := args[0]

	if err := handleRequest(configFile); err != nil {
		log.Fatalf("Request handling failed: %v", err)
	}
}
