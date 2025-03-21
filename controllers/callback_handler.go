package controllers

import (
	"MRContent/models"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/nats-io/nats.go"
	"github.com/praleedsuvarna/shared-libs/config"
	"github.com/praleedsuvarna/shared-libs/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// MediaProcessResult represents the result from media processing
// This structure must match the one in MediaProcessor service
type MediaProcessResult struct {
	ContentID      string `json:"content_id,omitempty"`
	OriginalURL    string `json:"original_url"`
	ProcessedURL   string `json:"processed_url"`
	HlsURL         string `json:"hls_url,omitempty"`
	DashURL        string `json:"dash_url,omitempty"`
	MediaType      string `json:"media_type"`            // "image", "video", "object_3d"
	ProcessingType string `json:"processing_type"`       // "compressed", "hls", "dash", "alpha", "stitched"
	Orientation    string `json:"orientation,omitempty"` // Added orientation field
	HasAlpha       bool   `json:"has_alpha,omitempty"`   // Added has_alpha field
	Success        bool   `json:"success"`
	Error          string `json:"error,omitempty"`
	Timestamp      int64  `json:"timestamp"`
}

// InitCallbackHandlers initializes HTTP and NATS listeners for media processing callbacks
func InitCallbackHandlers(app *fiber.App, nc *nats.Conn) error {
	// HTTP endpoint for callbacks
	app.Post("/api/media/callback", HandleMediaCallback)

	// NATS subscribers for various result topics
	if nc != nil {
		// Subscribe to all result topics
		topicPatterns := []string{
			"result.compressimage",
			"result.compressvideo",
			"result.transcodehlsdash",
			"result.generatealpha",
			"result.stitchvideos",
			"result.default",
		}

		for _, topic := range topicPatterns {
			_, err := nc.Subscribe(topic, func(msg *nats.Msg) {
				var result MediaProcessResult
				if err := json.Unmarshal(msg.Data, &result); err != nil {
					log.Printf("Error unmarshaling NATS message: %v", err)
					return
				}

				log.Printf("Received media processing result via NATS from topic %s: %+v", msg.Subject, result)

				// Process the result
				if err := processMediaResult(result); err != nil {
					log.Printf("Error processing media result from NATS: %v", err)
				}
			})

			if err != nil {
				return fmt.Errorf("error subscribing to NATS topic %s: %w", topic, err)
			}

			log.Printf("Subscribed to NATS topic: %s", topic)
		}
	} else {
		log.Println("Warning: NATS connection not provided, skipping NATS subscribers initialization")
	}

	return nil
}

// HandleMediaCallback processes HTTP callbacks from the MediaProcessor service
func HandleMediaCallback(c *fiber.Ctx) error {
	var result MediaProcessResult

	// Parse request body
	if err := c.BodyParser(&result); err != nil {
		log.Printf("Error parsing callback request: %v", err)
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error":   "Invalid request body",
		})
	}

	log.Printf("Received media processing result via HTTP: %+v", result)

	// Process the result
	if err := processMediaResult(result); err != nil {
		log.Printf("Error processing media result from HTTP: %v", err)
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"error":   err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Callback processed successfully",
	})
}

// processMediaResult updates the database with processed media URLs
func processMediaResult(result MediaProcessResult) error {
	// Skip processing if required fields are missing
	if result.ContentID == "" || (result.ProcessedURL == "" && result.HlsURL == "" && result.DashURL == "") {
		return fmt.Errorf("missing required fields: content_id or processed URL")
	}

	// Skip if processing was not successful
	if !result.Success {
		log.Printf("Media processing failed: %s", result.Error)
		// You might want to update the content status to "failed" or similar
		// Mark this task as complete even though it failed
		if err := TrackProcessingComplete(result.ContentID); err != nil {
			log.Printf("Error tracking processing completion for failed task: %v", err)
		}
		return nil
	}

	// Convert content ID from string to ObjectID
	contentID, err := primitive.ObjectIDFromHex(result.ContentID)
	if err != nil {
		return fmt.Errorf("invalid content ID format: %w", err)
	}

	// Get the content collection
	collection := config.GetCollection("oms_mrexperiences")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Get the current content
	var content models.MRContent
	err = collection.FindOne(ctx, bson.M{"_id": contentID}).Decode(&content)
	if err != nil {
		return fmt.Errorf("error finding content: %w", err)
	}

	// Prepare the update based on media type and processing type
	updateOps := bson.M{
		"updated_at": time.Now(),
	}

	// Update orientation and has_alpha if provided in result
	// These are set by the media processor during stitching or alpha processing
	if result.Orientation != "" {
		updateOps["orientation"] = result.Orientation
		log.Printf("Setting orientation to %s for content ID %s based on media processor feedback",
			result.Orientation, result.ContentID)
	}

	if result.HasAlpha {
		updateOps["has_alpha"] = true
		log.Printf("Setting has_alpha to true for content ID %s based on media processor feedback",
			result.ContentID)
	}

	// Handle different media types
	switch result.MediaType {
	case "image":
		// Process image as before
		if result.ProcessedURL != "" {
			// Update the images array
			existingImages := content.Images
			updatedImages := updateMediaField(existingImages, "compressed", result.ProcessedURL)
			updateOps["images"] = updatedImages
		}

	case "video":
		// Get existing videos
		existingVideos := content.Videos

		// Process based on the processing type
		switch result.ProcessingType {
		case "compressed":
			// Update the compressed video URL
			if result.ProcessedURL != "" {
				updatedVideos := updateMediaField(existingVideos, "compressed", result.ProcessedURL)
				updateOps["videos"] = updatedVideos
			}

		case "hls":
			// Handle both HLS and DASH URLs
			updatedVideos := existingVideos

			// Add HLS URL if available
			if result.HlsURL != "" {
				updatedVideos = updateMediaField(updatedVideos, "hls", result.HlsURL)
			}

			// Add DASH URL if available
			if result.DashURL != "" {
				updatedVideos = updateMediaField(updatedVideos, "dash", result.DashURL)
			}

			// Update videos array in database
			updateOps["videos"] = updatedVideos

		case "alpha":
			// Update the alpha video URL
			if result.ProcessedURL != "" {
				updatedVideos := updateMediaField(existingVideos, "alpha", result.ProcessedURL)
				updateOps["videos"] = updatedVideos
			}

		case "stitched":
			// Update the stitched video URL
			if result.ProcessedURL != "" {
				updatedVideos := updateMediaField(existingVideos, "stitched", result.ProcessedURL)
				updateOps["videos"] = updatedVideos
			}
		}

	case "object_3d":
		// Process 3D objects as before
		if result.ProcessedURL != "" {
			existingObjects := content.Objects_3D
			updatedObjects := updateMediaField(existingObjects, "processed", result.ProcessedURL)
			updateOps["objects_3d"] = updatedObjects
		}
	}

	// Update the document
	update := bson.M{"$set": updateOps}
	_, err = collection.UpdateOne(ctx, bson.M{"_id": contentID}, update)
	if err != nil {
		return fmt.Errorf("error updating content: %w", err)
	}

	// Log the action
	utils.LogAudit("system", fmt.Sprintf("Updated %s with %s URLs", result.MediaType, result.ProcessingType), result.ContentID)

	// Use multiple log statements with constant format strings instead of building a dynamic message
	log.Printf("Successfully updated content %s with %s %s URLs",
		result.ContentID, result.ProcessingType, result.MediaType)

	if result.Orientation != "" {
		log.Printf("Updated orientation to %s for content ID %s", result.Orientation, result.ContentID)
	}

	if result.HasAlpha {
		log.Printf("Updated has_alpha flag to true for content ID %s", result.ContentID)
	}

	// Log the type of processing that completed for debugging
	log.Printf("Completed %s processing for %s media, content ID: %s",
		result.ProcessingType, result.MediaType, result.ContentID)

	// Mark this processing task as complete
	if err := TrackProcessingComplete(result.ContentID); err != nil {
		log.Printf("Error tracking processing completion: %v", err)
	}

	return nil
}

// Helper function to update a media field
func updateMediaField(existingMedia []models.Media, key string, value string) []models.Media {
	// Check if the key already exists
	for i, media := range existingMedia {
		if media.Key == key {
			// Update existing key
			existingMedia[i].Value = value
			return existingMedia
		}
	}

	// Key doesn't exist, add it
	return append(existingMedia, models.Media{
		Key:   key,
		Value: value,
	})
}

// InitNATSSubscribers initializes NATS subscribers for media processing callbacks
func InitNATSSubscribers(nc *nats.Conn) error {
	if nc == nil {
		return fmt.Errorf("NATS connection is nil")
	}

	topicPatterns := []string{
		"result.compressimage",
		"result.compressvideo",
		"result.transcodehlsdash",
		"result.generatealpha",
		"result.stitchvideos",
		"result.default",
	}

	for _, topic := range topicPatterns {
		_, err := nc.Subscribe(topic, func(msg *nats.Msg) {
			var result MediaProcessResult
			if err := json.Unmarshal(msg.Data, &result); err != nil {
				log.Printf("Error unmarshaling NATS message: %v", err)
				return
			}

			log.Printf("Received media processing result via NATS from topic %s: %+v", msg.Subject, result)

			// Process the result
			if err := processMediaResult(result); err != nil {
				log.Printf("Error processing media result from NATS: %v", err)
			}
		})

		if err != nil {
			return fmt.Errorf("error subscribing to NATS topic %s: %w", topic, err)
		}

		log.Printf("Subscribed to NATS topic: %s", topic)
	}

	return nil
}
