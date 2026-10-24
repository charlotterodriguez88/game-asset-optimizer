package gameassets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

type ModerationState string

const (
	ModerationPending  ModerationState = "pending"
	ModerationApproved ModerationState = "approved"
	ModerationRejected ModerationState = "rejected"
)

type AssetRequest struct {
	PlayerID      string          `json:"player_id"`
	AssetID       string          `json:"asset_id"`
	EventID       string          `json:"event_id,omitempty"`
	Moderation    ModerationState `json:"moderation"`
	ContentBase64 string          `json:"content_base64"`
}

type QueueDecision struct {
	State    string `json:"state"`
	Priority string `json:"priority"`
	Reason   string `json:"reason"`
}

func DecideQueue(request AssetRequest) QueueDecision {
	switch request.Moderation {
	case ModerationRejected:
		return QueueDecision{State: "blocked", Priority: "none", Reason: "moderation rejected"}
	case ModerationPending:
		return QueueDecision{State: "moderation_queued", Priority: "normal", Reason: "moderation review required"}
	case ModerationApproved:
		priority := "normal"
		if request.EventID != "" {
			priority = "live"
		}
		return QueueDecision{State: "ready_to_optimize", Priority: priority, Reason: "approved asset"}
	default:
		return QueueDecision{State: "blocked", Priority: "none", Reason: "invalid moderation state"}
	}
}

type AssetResult struct {
	AssetID  string        `json:"asset_id"`
	Object   string        `json:"object_key,omitempty"`
	Decision QueueDecision `json:"decision"`
}

type MediaStore interface {
	EnsureBucket(context.Context, string) error
	Compress(context.Context, string, string) (string, error)
	PutObject(context.Context, string, string, string, string) error
}

type Pipeline struct {
	Client MediaStore
	Bucket string
}

func (p Pipeline) Initialize(ctx context.Context) error {
	return p.Client.EnsureBucket(ctx, p.Bucket)
}

func (p Pipeline) Process(ctx context.Context, request AssetRequest) (AssetResult, error) {
	decision := DecideQueue(request)
	result := AssetResult{AssetID: request.AssetID, Decision: decision}
	if decision.State != "ready_to_optimize" {
		return result, nil
	}
	if strings.TrimSpace(request.PlayerID) == "" || strings.TrimSpace(request.AssetID) == "" || request.ContentBase64 == "" {
		return AssetResult{}, fmt.Errorf("player_id, asset_id, and content_base64 are required")
	}
	objectKey := "players/" + request.PlayerID + "/" + request.AssetID + ".webp"
	requestID := stableRequestID(request, objectKey)
	optimized, err := p.Client.Compress(ctx, request.ContentBase64, requestID+":compress")
	if err != nil {
		return AssetResult{}, err
	}
	if optimized == "" {
		return AssetResult{}, fmt.Errorf("compression response contained no image data")
	}
	if err := p.Client.PutObject(ctx, p.Bucket, objectKey, optimized, requestID+":store"); err != nil {
		return AssetResult{}, err
	}
	result.Object = objectKey
	result.Decision.State = "stored"
	return result, nil
}

func stableRequestID(request AssetRequest, objectKey string) string {
	digest := sha256.Sum256([]byte(request.PlayerID + "\x00" + request.AssetID + "\x00" + objectKey))
	return "game-asset-" + hex.EncodeToString(digest[:12])
}
