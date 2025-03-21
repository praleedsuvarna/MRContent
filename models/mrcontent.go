package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Media represents an image or video with a key-value structure
type Media struct {
	Key   string `bson:"k" json:"k"`
	Value string `bson:"v" json:"v"`
}

type MRContent struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	OrganizationID primitive.ObjectID `bson:"organization_id,omitempty" json:"organization_id,omitempty"`
	UserID         primitive.ObjectID `bson:"user_id,omitempty" json:"user_id,omitempty"`
	Name           string             `bson:"name,omitempty" json:"name,omitempty"`
	RefID          string             `bson:"ref_id,omitempty" json:"ref_id,omitempty"`
	RenderType     string             `bson:"render_type" json:"render_type"`
	Images         []Media            `bson:"images,omitempty" json:"images,omitempty"`
	Videos         []Media            `bson:"videos,omitempty" json:"videos,omitempty"`
	Objects_3D     []Media            `bson:"objects_3d,omitempty" json:"objects_3d,omitempty"`
	HasAlpha       bool               `bson:"has_alpha" json:"has_alpha"`
	Orientation    string             `bson:"orientation,omitempty" json:"orientation,omitempty"`
	Status         string             `bson:"status" json:"status"`
	IsActive       bool               `bson:"is_active" json:"is_active"`
	CreatedAt      time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt      time.Time          `bson:"updated_at" json:"updated_at"`
}
