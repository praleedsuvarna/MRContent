package controllers

import (
	"MRContent/models"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/nats-io/nats.go"
	"github.com/praleedsuvarna/shared-libs/config"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

var (
	// Singleton NATS connection
	natsConn *nats.Conn
	natsOnce sync.Once
	natsErr  error
)

// TranscodeRequest matches the structure expected by the media processing service
type TranscodeRequest struct {
	VideoURL       string `json:"video_url,omitempty"`
	ImageURL       string `json:"image_url,omitempty"`
	AlphaVideoURL  string `json:"alphavideo_url,omitempty"`
	ContentID      string `json:"content_id,omitempty"`
	CallbackURL    string `json:"callback_url,omitempty"`
	CallbackTopic  string `json:"callback_topic,omitempty"`
	OrganizationID string `json:"organization_id,omitempty"`
}

// InitNATS initializes the NATS connection
func InitNATS() (*nats.Conn, error) {
	natsOnce.Do(func() {
		// Get NATS URL from environment
		natsURL := os.Getenv("NATS_URL")
		if natsURL == "" {
			// Fallback to default if not set
			natsURL = "nats://localhost:4222"
			log.Printf("NATS_URL not found in environment, using default: %s", natsURL)
		}

		// Connect to NATS
		nc, err := nats.Connect(natsURL)
		if err != nil {
			natsErr = fmt.Errorf("failed to connect to NATS: %w", err)
			log.Printf("NATS connection error: %v", natsErr)
			return
		}

		log.Printf("Successfully connected to NATS server at %s", natsURL)
		natsConn = nc
	})

	return natsConn, natsErr
}

// GetNATS returns the singleton NATS connection, initializing it if needed
func GetNATS() (*nats.Conn, error) {
	if natsConn != nil {
		return natsConn, nil
	}
	return InitNATS()
}

// CloseNATS closes the NATS connection
func CloseNATS() {
	if natsConn != nil {
		natsConn.Close()
		natsConn = nil
	}
}

// ProcessMediaForContent handles media processing for a newly created MR content
func ProcessMediaForContent(content models.MRContent) {
	// Get NATS connection
	nc, err := GetNATS()
	if err != nil {
		log.Printf("Error getting NATS connection, cannot process media: %v", err)
		return
	}

	// Count how many processing tasks will be needed
	taskCount := CountMediaTasks(content)

	if taskCount <= 0 {
		log.Printf("No media processing tasks identified for content ID: %s", content.ID.Hex())
		return
	}

	// Start tracking and update status to "processing"
	contentIDStr := content.ID.Hex()
	if err := TrackProcessingStart(contentIDStr, taskCount); err != nil {
		log.Printf("Error tracking processing start: %v", err)
		// Continue with processing anyway
	}

	// Process media assets asynchronously
	go func() {
		// contentIDStr := content.ID.Hex()
		orgIDStr := content.OrganizationID.Hex()
		log.Printf("Starting media processing for content ID: %s, organization ID: %s", contentIDStr, orgIDStr)

		// Process images if any
		for _, img := range content.Images {
			if strings.HasPrefix(img.Key, "original") && img.Value != "" {
				log.Printf("Processing original image: %s", img.Value)

				// Create a request with ContentID
				request := TranscodeRequest{
					ImageURL:       img.Value,
					ContentID:      contentIDStr,
					OrganizationID: orgIDStr,
				}

				// Process the image
				processImage(nc, request)
			}
		}

		// Process videos if any
		// Look for both original and mask videos
		var originalVideoURL string
		var maskVideoURL string

		// First, find the original video URL
		for _, video := range content.Videos {
			if strings.HasPrefix(video.Key, "original") && video.Value != "" {
				originalVideoURL = video.Value
				break
			}
		}

		// Then, find the mask video URL (if exists)
		for _, video := range content.Videos {
			if strings.HasPrefix(video.Key, "mask") && video.Value != "" {
				maskVideoURL = video.Value
				break
			}
		}

		// If we have an original video, proceed with processing
		if originalVideoURL != "" {
			log.Printf("Processing original video: %s", originalVideoURL)

			// Create base request with ContentID
			baseRequest := TranscodeRequest{
				VideoURL:       originalVideoURL,
				ContentID:      contentIDStr,
				OrganizationID: orgIDStr,
			}

			// If we have a mask video, add it to the request
			if maskVideoURL != "" {
				log.Printf("Found mask video: %s", maskVideoURL)
				baseRequest.AlphaVideoURL = maskVideoURL
			}

			// Publish to createexperience topic for combined processing
			if err := publishToNATS(nc, baseRequest, "createexperience"); err != nil {
				log.Printf("Error publishing to createexperience: %v", err)

				// Mark all video tasks as complete since they failed
				// Each video normally accounts for multiple tasks (potentially stitching + compression + HLS/DASH)
				// We need to complete them all on error
				for i := 0; i < 3; i++ {
					if err := TrackProcessingComplete(contentIDStr); err != nil {
						log.Printf("Error marking failed task as complete: %v", err)
					}
				}
			} else {
				log.Printf("Published to createexperience topic for content ID: %s", contentIDStr)
			}
		}
		// Process videos if any
		// for _, video := range content.Videos {
		// 	if strings.HasPrefix(video.Key, "original") && video.Value != "" {
		// 		log.Printf("Processing original video: %s", video.Value)

		// 		// Create base request with ContentID
		// 		baseRequest := TranscodeRequest{
		// 			VideoURL:       video.Value,
		// 			ContentID:      contentIDStr,
		// 			OrganizationID: orgIDStr,
		// 		}

		// 		// Process for HLS/DASH streaming
		// 		processVideoHLSDASH(nc, baseRequest)

		// 		// Process for compression
		// 		processVideoCompression(nc, baseRequest)

		// 		// Check if we need alpha video processing
		// 		if content.HasAlpha {
		// 			// Find the alpha video URL
		// 			var alphaVideoURL string
		// 			for _, alphaVideo := range content.Videos {
		// 				if strings.HasPrefix(alphaVideo.Key, "original_alpha") && alphaVideo.Value != "" {
		// 					alphaVideoURL = alphaVideo.Value
		// 					break
		// 				}
		// 			}

		// 			// Generate alpha if not already available
		// 			if alphaVideoURL == "" {
		// 				log.Printf("Generating alpha channel for video: %s", video.Value)
		// 				generateAlphaVideo(nc, baseRequest)
		// 			} else {
		// 				// If both normal and alpha videos are available, stitch them
		// 				log.Printf("Stitching video with alpha: %s + %s", video.Value, alphaVideoURL)

		// 				// Create request with both videos and ContentID
		// 				stitchRequest := TranscodeRequest{
		// 					VideoURL:       video.Value,
		// 					AlphaVideoURL:  alphaVideoURL,
		// 					ContentID:      contentIDStr,
		// 					OrganizationID: orgIDStr,
		// 				}

		// 				stitchVideos(nc, stitchRequest)
		// 			}
		// 		}
		// 	}
		// }

		// Process 3D objects if any
		for _, obj := range content.Objects_3D {
			if strings.HasPrefix(obj.Key, "original") && obj.Value != "" {
				log.Printf("Processing original 3D object: %s", obj.Value)
				// Add specific processing for 3D objects here if needed
				log.Printf("Found 3D object to process: %s", obj.Value)
			}
		}

		log.Printf("Completed queueing media processing tasks for content ID: %s", contentIDStr)
	}()
}

// processImage sends a request to compress an image
func processImage(nc *nats.Conn, request TranscodeRequest) {
	// Ensure we're using the request object directly
	if request.ImageURL == "" {
		log.Printf("Error: Missing image URL in request")
		return
	}

	// Publish to NATS subject
	if err := publishToNATS(nc, request, "compressimage"); err != nil {
		log.Printf("Error publishing image compression request: %v", err)
		return
	}

	log.Printf("Image compression request published for content ID: %s, image: %s",
		request.ContentID, request.ImageURL)
}

// processVideoHLSDASH sends a request to transcode a video for HLS/DASH streaming
func processVideoHLSDASH(nc *nats.Conn, request TranscodeRequest) {
	// Ensure we're using the request object directly
	if request.VideoURL == "" {
		log.Printf("Error: Missing video URL in request")
		return
	}

	// Publish to NATS subject
	if err := publishToNATS(nc, request, "transcodehlsdash"); err != nil {
		log.Printf("Error publishing HLS/DASH transcode request: %v", err)
		return
	}

	log.Printf("HLS/DASH transcode request published for content ID: %s, video: %s",
		request.ContentID, request.VideoURL)
}

// processVideoCompression sends a request to compress a video
func processVideoCompression(nc *nats.Conn, request TranscodeRequest) {
	// Ensure we're using the request object directly
	if request.VideoURL == "" {
		log.Printf("Error: Missing video URL in request")
		return
	}

	// Publish to NATS subject
	if err := publishToNATS(nc, request, "compressvideo"); err != nil {
		log.Printf("Error publishing video compression request: %v", err)
		return
	}

	log.Printf("Video compression request published for content ID: %s, video: %s",
		request.ContentID, request.VideoURL)
}

// generateAlphaVideo sends a request to generate an alpha channel video
func generateAlphaVideo(nc *nats.Conn, request TranscodeRequest) {
	// Ensure we're using the request object directly
	if request.VideoURL == "" {
		log.Printf("Error: Missing video URL in request")
		return
	}

	// Publish to NATS subject
	if err := publishToNATS(nc, request, "generatealpha"); err != nil {
		log.Printf("Error publishing generate alpha request: %v", err)
		return
	}

	log.Printf("Generate alpha request published for content ID: %s, video: %s",
		request.ContentID, request.VideoURL)
}

// stitchVideos sends a request to stitch normal and alpha videos together
func stitchVideos(nc *nats.Conn, request TranscodeRequest) {
	// Ensure we're using the request object directly
	if request.VideoURL == "" || request.AlphaVideoURL == "" {
		log.Printf("Error: Missing video or alpha video URL in request")
		return
	}

	// Publish to NATS subject
	if err := publishToNATS(nc, request, "stitchvideos"); err != nil {
		log.Printf("Error publishing stitch videos request: %v", err)
		return
	}

	log.Printf("Stitch videos request published for content ID: %s, videos: %s + %s",
		request.ContentID, request.VideoURL, request.AlphaVideoURL)
}

// publishToNATS is a helper function to publish requests to NATS
func publishToNATS(nc *nats.Conn, data interface{}, subject string) error {
	// Create JSON payload
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("error marshaling request: %w", err)
	}

	// Publish to NATS subject
	return nc.Publish(subject, jsonData)
}

// Get media collection
func GetMediaCollection() *mongo.Collection {
	return config.GetCollection("oms_mrexperiences")
}

// Get content by ID
func GetContentByID(ctx context.Context, contentID primitive.ObjectID) (models.MRContent, error) {
	var content models.MRContent
	collection := GetMediaCollection()
	err := collection.FindOne(ctx, bson.M{"_id": contentID}).Decode(&content)
	return content, err
}
