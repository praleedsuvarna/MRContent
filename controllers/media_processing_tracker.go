package controllers

import (
	"MRContent/models"
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/praleedsuvarna/shared-libs/config"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// MediaProcessingTracker keeps track of ongoing media processing tasks
type MediaProcessingTracker struct {
	mutex           sync.Mutex
	processingTasks map[string]int // Maps contentID to count of pending tasks
}

// NewMediaProcessingTracker creates a new tracker
func NewMediaProcessingTracker() *MediaProcessingTracker {
	return &MediaProcessingTracker{
		processingTasks: make(map[string]int),
	}
}

// Global instance of the tracker
var mediaTracker = NewMediaProcessingTracker()

// TrackProcessingStart registers the start of media processing for a content item
// and updates its status to "processing"
func TrackProcessingStart(contentID string, taskCount int) error {
	if taskCount <= 0 {
		return nil // No tasks to track
	}

	// Update tracker
	mediaTracker.mutex.Lock()
	mediaTracker.processingTasks[contentID] = taskCount
	mediaTracker.mutex.Unlock()

	// Update content status to "processing"
	objContentID, err := primitive.ObjectIDFromHex(contentID)
	if err != nil {
		return err
	}

	collection := config.GetCollection("oms_mrexperiences")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// First check current status to avoid unnecessary updates
	var content models.MRContent
	err = collection.FindOne(ctx, bson.M{"_id": objContentID}).Decode(&content)
	if err != nil {
		log.Printf("Error finding content: %v", err)
		return err
	}

	// Only update to "processing" if it's currently "draft"
	if content.Status == "draft" {
		updateData := bson.M{
			"$set": bson.M{
				"status":     "processing",
				"updated_at": time.Now(),
			},
		}

		result, err := collection.UpdateOne(
			ctx,
			bson.M{"_id": objContentID, "status": "draft"},
			updateData,
		)

		if err != nil {
			log.Printf("Error updating content status to processing: %v", err)
			return err
		}

		if result.ModifiedCount > 0 {
			log.Printf("Content %s status changed to 'processing', tracking %d tasks", contentID, taskCount)
		} else {
			log.Printf("Content %s status was not updated (may not be in 'draft' state)", contentID)
		}
	} else {
		log.Printf("Content %s is already in '%s' state, not changing to 'processing'", contentID, content.Status)
	}

	return nil
}

// TrackProcessingComplete registers the completion of a media processing task
// and updates content status to "processed" when all tasks are done
func TrackProcessingComplete(contentID string) error {
	// Update tracker
	mediaTracker.mutex.Lock()

	// If no tracking entry exists, nothing to do
	if _, exists := mediaTracker.processingTasks[contentID]; !exists {
		mediaTracker.mutex.Unlock()
		log.Printf("No tracking information found for content ID: %s", contentID)
		return nil
	}

	// Decrement task count
	mediaTracker.processingTasks[contentID]--
	remainingTasks := mediaTracker.processingTasks[contentID]

	// If no tasks remaining, remove from tracker
	if remainingTasks <= 0 {
		delete(mediaTracker.processingTasks, contentID)
		log.Printf("All processing tasks completed for content ID: %s", contentID)
	} else {
		log.Printf("Content %s has %d remaining processing tasks", contentID, remainingTasks)
		mediaTracker.mutex.Unlock()
		return nil
	}

	mediaTracker.mutex.Unlock()

	// All tasks complete, update content status to "processed"
	objContentID, err := primitive.ObjectIDFromHex(contentID)
	if err != nil {
		return err
	}

	collection := config.GetCollection("oms_mrexperiences")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// First check current status to make sure we're only updating from "processing" state
	var content models.MRContent
	err = collection.FindOne(ctx, bson.M{"_id": objContentID}).Decode(&content)
	if err != nil {
		log.Printf("Error finding content: %v", err)
		return err
	}

	// Only update to "processed" if it's currently "processing"
	if content.Status == "processing" {
		updateData := bson.M{
			"$set": bson.M{
				"status":     "processed",
				"updated_at": time.Now(),
			},
		}

		result, err := collection.UpdateOne(
			ctx,
			bson.M{"_id": objContentID, "status": "processing"},
			updateData,
		)

		if err != nil {
			log.Printf("Error updating content status to processed: %v", err)
			return err
		}

		if result.ModifiedCount > 0 {
			log.Printf("Content %s status changed to 'processed', all tasks completed", contentID)
		} else {
			log.Printf("Content %s status was not updated to 'processed', may have been manually changed", contentID)
		}
	} else {
		log.Printf("Content %s is in '%s' state, not changing to 'processed'", contentID, content.Status)
	}

	return nil
}

// GetProcessingStatus returns the current processing status for a content item
func GetProcessingStatus(contentID string) (string, int, error) {
	// First check if it's still in our tracker
	mediaTracker.mutex.Lock()
	remainingTasks, exists := mediaTracker.processingTasks[contentID]
	mediaTracker.mutex.Unlock()

	if exists {
		return "processing", remainingTasks, nil
	}

	// If not in tracker, check database
	objContentID, err := primitive.ObjectIDFromHex(contentID)
	if err != nil {
		return "", 0, err
	}

	collection := config.GetCollection("oms_mrexperiences")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var content models.MRContent
	err = collection.FindOne(ctx, bson.M{"_id": objContentID}).Decode(&content)
	if err != nil {
		return "", 0, err
	}

	return content.Status, 0, nil
}

// CountMediaTasks counts the number of media processing tasks needed for a content item
func CountMediaTasks(content models.MRContent) int {
	taskCount := 0

	// Count image processing tasks
	imgCount := 0
	for _, img := range content.Images {
		if strings.HasPrefix(img.Key, "original") && img.Value != "" {
			imgCount++
		}
	}

	if imgCount > 0 {
		taskCount += imgCount
	}

	// Count video processing tasks
	videoCount := 0
	alphaVideoCount := 0

	for _, video := range content.Videos {
		if strings.HasPrefix(video.Key, "original") && video.Value != "" {
			videoCount++
		}
		if video.Key == "mask" || video.Key == "original_alpha" {
			alphaVideoCount++
		}
	}

	// Video processing complexity varies based on what's needed
	if videoCount > 0 {
		// Based on MediaProcessor, we might need multiple processing steps:
		// 1. For each original video: potentially stitching (if alpha exists)
		// 2. For each original video: HLS/DASH generation
		// 3. For each original video: compression

		if alphaVideoCount > 0 {
			// If we have both original and alpha videos, they'll be stitched
			taskCount += 1 // Stitching task
		}

		// Always count compression and HLS/DASH tasks separately
		// These run as separate asynchronous processes in the MediaProcessor
		taskCount += videoCount * 2 // Compression + HLS/DASH for each video
	}

	// Count 3D object processing tasks if any
	// Currently no processing for 3D objects in the code

	log.Printf("Counted %d total processing tasks for content ID: %s", taskCount, content.ID.Hex())
	return taskCount
}
