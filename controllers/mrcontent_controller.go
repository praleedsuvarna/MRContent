package controllers

import (
	"MRContent/models"
	"encoding/json"
	"log"
	"math/rand"
	"strings"

	// "MRContent/utils"
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/praleedsuvarna/shared-libs/config"
	"github.com/praleedsuvarna/shared-libs/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// CreateMRContent creates a new MR content record
func CreateMRContent(c *fiber.Ctx) error {
	// Get user and organization ID from token context
	userID := c.Locals("user_id").(string)
	orgID := c.Locals("organization_id").(string)

	var content models.MRContent
	if err := c.BodyParser(&content); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	// Convert IDs to ObjectID
	objUserID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	objOrgID, err := primitive.ObjectIDFromHex(orgID)
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "Invalid organization ID"})
	}

	// Get collection
	collection := config.GetCollection("oms_mrexperiences")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Generate a unique 6-digit alphanumeric ref_id
	var isUnique bool
	var refID string
	maxAttempts := 10
	for attempt := 0; attempt < maxAttempts; attempt++ {
		refID = generateUniqueRefID()

		// Check if refID already exists
		count, err := collection.CountDocuments(ctx, bson.M{"ref_id": refID})
		if err != nil {
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": "Error checking ref_id uniqueness"})
		}

		if count == 0 {
			isUnique = true
			break
		}
	}

	if !isUnique {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to generate unique ref_id after multiple attempts"})
	}

	// Set metadata
	content.ID = primitive.NewObjectID()
	content.UserID = objUserID
	content.OrganizationID = objOrgID
	content.RefID = refID
	currentTime := time.Now()
	content.CreatedAt = currentTime
	content.UpdatedAt = currentTime
	content.IsActive = true

	// If status is not provided, set it to "draft"
	if content.Status == "" {
		content.Status = "draft"
	}

	// Insert document
	_, err = collection.InsertOne(ctx, content)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	// Log the action
	utils.LogAudit(userID, "Created MR content", content.ID.Hex())

	// Check if there are any media assets to process
	hasMedia := (len(content.Images) > 0 || len(content.Videos) > 0 || len(content.Objects_3D) > 0)

	// Process media assets if any exist
	if hasMedia {
		go ProcessMediaForContent(content) // Process media in the background
		log.Printf("Media processing triggered for content ID: %s", content.ID.Hex())
	}

	response := transformMRContentResponse(content)
	return c.Status(http.StatusCreated).JSON(response)
}

// GetMRContent retrieves a specific MR content by ID
func GetMRContent(c *fiber.Ctx) error {
	// Get content ID from params
	contentID := c.Params("id")
	if contentID == "" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "Content ID is required"})
	}

	// Get user's organization ID from token
	orgID := c.Locals("organization_id").(string)
	objOrgID, err := primitive.ObjectIDFromHex(orgID)
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "Invalid organization ID"})
	}

	// Convert to ObjectID
	objContentID, err := primitive.ObjectIDFromHex(contentID)
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "Invalid content ID format"})
	}

	// Get collection
	collection := config.GetCollection("oms_mrexperiences")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Find document
	var content models.MRContent
	err = collection.FindOne(ctx, bson.M{
		"_id":             objContentID,
		"organization_id": objOrgID,
		"is_active":       true,
	}).Decode(&content)

	if err != nil {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{"error": "MR content not found"})
	}

	// Transform the response to add flattened media
	response := transformMRContentResponse(content)

	return c.JSON(response)
}

// UpdateMRContent updates an existing MR content
// UpdateMRContent updates only the provided fields in an existing MR content
func UpdateMRContent(c *fiber.Ctx) error {
	// Get content ID from params
	contentID := c.Params("id")
	if contentID == "" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "Content ID is required"})
	}

	// Get user and organization ID from token
	userID := c.Locals("user_id").(string)
	orgID := c.Locals("organization_id").(string)

	// Read the raw body
	requestBody := c.Body()

	// Parse request body
	var updateContent models.MRContent
	if err := c.BodyParser(&updateContent); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	// Convert IDs to ObjectID
	objContentID, err := primitive.ObjectIDFromHex(contentID)
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "Invalid content ID format"})
	}

	objOrgID, err := primitive.ObjectIDFromHex(orgID)
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "Invalid organization ID"})
	}

	// Get collection
	collection := config.GetCollection("oms_mrexperiences")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Check if the content exists and belongs to the organization
	var existingContent models.MRContent
	err = collection.FindOne(ctx, bson.M{
		"_id":             objContentID,
		"organization_id": objOrgID,
		"is_active":       true,
	}).Decode(&existingContent)

	if err != nil {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{"error": "MR content not found"})
	}

	// Create map to store new media URLs that need processing
	newMediaForProcessing := make(map[string]string)

	// Update media arrays if provided
	updatedImages := existingContent.Images
	if updateContent.Images != nil {
		// Check for new original media URLs before merging
		existingImageURLs := make(map[string]string)
		for _, img := range existingContent.Images {
			if strings.HasPrefix(img.Key, "original") {
				existingImageURLs[img.Key] = img.Value
			}
		}

		// Find new or changed original images
		for _, img := range updateContent.Images {
			if strings.HasPrefix(img.Key, "original") {
				existingURL, exists := existingImageURLs[img.Key]
				if !exists || existingURL != img.Value {
					// This is a new or modified original image URL
					newMediaForProcessing[img.Key] = img.Value
				}
			}
		}

		// Merge media
		updatedImages = mergeMediaByKey(existingContent.Images, updateContent.Images)
	}

	updatedVideos := existingContent.Videos
	if updateContent.Videos != nil {
		// Check for new original media URLs before merging
		existingVideoURLs := make(map[string]string)
		for _, video := range existingContent.Videos {
			if strings.HasPrefix(video.Key, "original") {
				existingVideoURLs[video.Key] = video.Value
			}
		}

		// Find new or changed original videos
		for _, video := range updateContent.Videos {
			if strings.HasPrefix(video.Key, "original") {
				existingURL, exists := existingVideoURLs[video.Key]
				if !exists || existingURL != video.Value {
					// This is a new or modified original video URL
					newMediaForProcessing[video.Key] = video.Value
				}
			}
		}

		// Merge media
		updatedVideos = mergeMediaByKey(existingContent.Videos, updateContent.Videos)
	}

	updatedObjects3D := existingContent.Objects_3D
	if updateContent.Objects_3D != nil {
		// Check for new original media URLs before merging
		existingObjectURLs := make(map[string]string)
		for _, obj := range existingContent.Objects_3D {
			if strings.HasPrefix(obj.Key, "original") {
				existingObjectURLs[obj.Key] = obj.Value
			}
		}

		// Find new or changed original 3D objects
		for _, obj := range updateContent.Objects_3D {
			if strings.HasPrefix(obj.Key, "original") {
				existingURL, exists := existingObjectURLs[obj.Key]
				if !exists || existingURL != obj.Value {
					// This is a new or modified original 3D object URL
					newMediaForProcessing[obj.Key] = obj.Value
				}
			}
		}

		// Merge media
		updatedObjects3D = mergeMediaByKey(existingContent.Objects_3D, updateContent.Objects_3D)
	}

	// Create update set with only provided fields
	updateSet := bson.M{
		"images":     updatedImages,
		"videos":     updatedVideos,
		"objects_3d": updatedObjects3D,
		"updated_at": time.Now(),
	}

	// Only update other fields if they're provided (not empty)
	if updateContent.Name != "" {
		updateSet["name"] = updateContent.Name
	}

	if updateContent.RenderType != "" {
		updateSet["render_type"] = updateContent.RenderType
	}

	if updateContent.Orientation != "" {
		updateSet["orientation"] = updateContent.Orientation
	}

	if updateContent.Status != "" {
		updateSet["status"] = updateContent.Status
	}

	// Check if HasAlpha was explicitly provided in the request
	var rawBody map[string]interface{}
	if err := json.Unmarshal(requestBody, &rawBody); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "Failed to parse request body"})
	}

	// Only update HasAlpha if it was explicitly provided in the request
	if _, hasAlphaExists := rawBody["has_alpha"]; hasAlphaExists {
		updateSet["has_alpha"] = updateContent.HasAlpha
	}

	// Prepare update document
	updateData := bson.M{
		"$set": updateSet,
	}

	// Update the document
	_, err = collection.UpdateOne(
		ctx,
		bson.M{"_id": objContentID, "organization_id": objOrgID},
		updateData,
	)

	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update MR content"})
	}

	// Log the action
	utils.LogAudit(userID, "Updated MR content", contentID)

	// Get updated content
	var updatedContent models.MRContent
	err = collection.FindOne(ctx, bson.M{"_id": objContentID}).Decode(&updatedContent)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve updated content"})
	}

	// If new or changed original media was added, trigger media processing
	if len(newMediaForProcessing) > 0 {
		log.Printf("New or changed media detected for content ID %s. Processing %d media items",
			updatedContent.ID.Hex(), len(newMediaForProcessing))

		// Create a content object with only the new media to process
		contentToProcess := models.MRContent{
			ID:             updatedContent.ID,
			OrganizationID: updatedContent.OrganizationID,
			UserID:         updatedContent.UserID,
			HasAlpha:       updatedContent.HasAlpha,
		}

		// Add only the new/changed images to process
		for _, img := range updatedImages {
			if _, exists := newMediaForProcessing[img.Key]; exists && strings.HasPrefix(img.Key, "original") {
				contentToProcess.Images = append(contentToProcess.Images, img)
				log.Printf("Added new/changed image for processing: %s = %s", img.Key, img.Value)
			}
		}

		// Add only the new/changed videos to process
		for _, video := range updatedVideos {
			if _, exists := newMediaForProcessing[video.Key]; exists && strings.HasPrefix(video.Key, "original") {
				contentToProcess.Videos = append(contentToProcess.Videos, video)
				log.Printf("Added new/changed video for processing: %s = %s", video.Key, video.Value)
			}
		}

		// Add only the new/changed 3D objects to process
		for _, obj := range updatedObjects3D {
			if _, exists := newMediaForProcessing[obj.Key]; exists && strings.HasPrefix(obj.Key, "original") {
				contentToProcess.Objects_3D = append(contentToProcess.Objects_3D, obj)
				log.Printf("Added new/changed 3D object for processing: %s = %s", obj.Key, obj.Value)
			}
		}

		// Process only the new or changed media
		go ProcessMediaForContent(contentToProcess)
		log.Printf("Media processing triggered for updated content ID: %s with selective media", updatedContent.ID.Hex())
	} else {
		log.Printf("No new media detected for content ID %s. Skipping media processing", updatedContent.ID.Hex())
	}

	// Return transformed response
	response := transformMRContentResponse(updatedContent)
	return c.JSON(response)
}

// mergeMediaByKey merges new media items with existing ones based on keys
// If a key exists, it updates the value; if not, it adds the new key-value pair
func mergeMediaByKey(existingMedia, newMedia []models.Media) []models.Media {
	// If no new media is provided, return existing media unchanged
	if len(newMedia) == 0 {
		return existingMedia
	}

	// If there is no existing media, just return the new media
	if existingMedia == nil {
		return newMedia
	}

	// Create a map for easy lookup of existing media by key
	mediaMap := make(map[string]string)
	for _, item := range existingMedia {
		mediaMap[item.Key] = item.Value
	}

	// Update or add new media items
	for _, item := range newMedia {
		mediaMap[item.Key] = item.Value
	}

	// Convert map back to slice
	result := make([]models.Media, 0, len(mediaMap))
	for k, v := range mediaMap {
		result = append(result, models.Media{
			Key:   k,
			Value: v,
		})
	}

	return result
}

// DeleteMRContent soft-deletes an MR content (sets is_active to false)
func DeleteMRContent(c *fiber.Ctx) error {
	// Get content ID from params
	contentID := c.Params("id")
	if contentID == "" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "Content ID is required"})
	}

	// Get user and organization ID from token
	userID := c.Locals("user_id").(string)
	orgID := c.Locals("organization_id").(string)

	// Convert IDs to ObjectID
	objContentID, err := primitive.ObjectIDFromHex(contentID)
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "Invalid content ID format"})
	}

	objOrgID, err := primitive.ObjectIDFromHex(orgID)
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "Invalid organization ID"})
	}

	// Get collection
	collection := config.GetCollection("oms_mrexperiences")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Perform soft delete (set is_active to false)
	updateData := bson.M{
		"$set": bson.M{
			"is_active":  false,
			"updated_at": time.Now(),
		},
	}

	result, err := collection.UpdateOne(
		ctx,
		bson.M{"_id": objContentID, "organization_id": objOrgID, "is_active": true},
		updateData,
	)

	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete MR content"})
	}

	if result.ModifiedCount == 0 {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{"error": "MR content not found"})
	}

	// Log the action
	utils.LogAudit(userID, "Deleted MR content", contentID)

	return c.JSON(fiber.Map{"message": "MR content deleted successfully"})
}

// ListMRContents retrieves all MR contents for the organization
func ListMRContents(c *fiber.Ctx) error {
	// Get organization ID from token
	orgID := c.Locals("organization_id").(string)
	objOrgID, err := primitive.ObjectIDFromHex(orgID)
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "Invalid organization ID"})
	}

	// Parse query parameters
	limit := 10
	if c.Query("limit") != "" {
		_, err := fmt.Sscanf(c.Query("limit"), "%d", &limit)
		if err != nil || limit < 1 {
			limit = 10 // Default to 10 if invalid
		}
	}

	skip := 0
	if c.Query("page") != "" {
		page := 0
		_, err := fmt.Sscanf(c.Query("page"), "%d", &page)
		if err == nil && page > 0 {
			skip = (page - 1) * limit
		}
	}

	status := c.Query("status")
	renderType := c.Query("render_type")

	// Get collection
	collection := config.GetCollection("oms_mrexperiences")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Build filter
	filter := bson.M{
		"organization_id": objOrgID,
		"is_active":       true,
	}

	// Add status filter if provided
	if status != "" {
		filter["status"] = status
	}
	if renderType != "" {
		filter["render_type"] = renderType
	}

	// Set options for pagination
	findOptions := options.Find()
	findOptions.SetLimit(int64(limit))
	findOptions.SetSkip(int64(skip))
	findOptions.SetSort(bson.M{"created_at": -1}) // Sort by created_at desc

	// Execute query
	cursor, err := collection.Find(ctx, filter, findOptions)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	defer cursor.Close(ctx)

	// Decode results
	var contents []models.MRContent
	if err := cursor.All(ctx, &contents); err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	// Transform each content item to include flattened media
	var transformedContents []map[string]interface{}
	for _, content := range contents {
		transformedContents = append(transformedContents, transformMRContentResponse(content))
	}

	// Get total count for pagination
	total, err := collection.CountDocuments(ctx, filter)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to count documents"})
	}

	return c.JSON(fiber.Map{
		"data":        transformedContents,
		"total":       total,
		"page":        skip/limit + 1,
		"page_size":   limit,
		"total_pages": (total + int64(limit) - 1) / int64(limit),
	})
}

// transformMRContentResponse converts MRContent to a response with flattened media
func transformMRContentResponse(content models.MRContent) map[string]interface{} {
	// Create the base response
	response := map[string]interface{}{
		"id":              content.ID,
		"organization_id": content.OrganizationID,
		"user_id":         content.UserID,
		"name":            content.Name,
		"ref_id":          content.RefID,
		"render_type":     content.RenderType,
		"has_alpha":       content.HasAlpha,
		"orientation":     content.Orientation,
		"status":          content.Status,
		"is_active":       content.IsActive,
		"created_at":      content.CreatedAt,
		"updated_at":      content.UpdatedAt,
	}

	// // Add the original arrays
	// response["images"] = content.Images
	// response["videos"] = content.Videos

	// Add flattened images with prefix
	for _, img := range content.Images {
		key := "images_" + img.Key
		response[key] = img.Value
	}

	// Add flattened videos with prefix
	for _, vid := range content.Videos {
		key := "videos_" + vid.Key
		response[key] = vid.Value
	}

	// Add flattened 3D objects with prefix
	for _, obj := range content.Objects_3D {
		key := "objects_3d_" + obj.Key
		response[key] = obj.Value
	}

	return response
}

// generateUniqueRefID creates a 6-digit alphanumeric and special character reference ID
// that is browser-compatible
func generateUniqueRefID() string {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_~.@#$%^&*()+=!"
	const length = 6

	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	result := strings.Builder{}
	result.Grow(length)

	for i := 0; i < length; i++ {
		index := r.Intn(len(charset))
		result.WriteByte(charset[index])
	}

	return result.String()
}
