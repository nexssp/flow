package flow

import (
	"context"
	"fmt"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

type LocationRegion string

const (
	RegionEU   LocationRegion = "eu-central-1"
	RegionUS   LocationRegion = "us-east-1"
	RegionAsia LocationRegion = "ap-southeast-1"
)

type EdgeNodeReq struct {
	Region  LocationRegion `json:"region"`
	Payload map[string]any `json:"payload"`
}

type EdgeNodeRes struct {
	Region     LocationRegion `json:"region"`
	NodeID     string         `json:"node_id"`
	LatencyMs  int64          `json:"latency_ms"`
	Data       map[string]any `json:"data"`
	StatusCode uint16         `json:"status_code"`
}

func BuildEdgeMeshAction(region LocationRegion, nodeID string, ring *EdgeTelemetryRing) action.AnyAction {
	var rawID [16]byte
	copy(rawID[:], nodeID)

	actionName := fmt.Sprintf("edge.%s", string(region))

	return action.New(actionName, func(ctx context.Context, req map[string]any) (EdgeNodeRes, error) {
		start := time.Now()

		select {
		case <-ctx.Done():
			ring.Push(rawID, time.Since(start).Nanoseconds(), 504)

			return EdgeNodeRes{}, ctx.Err()
		default:
		}

		if req == nil {
			ring.Push(rawID, time.Since(start).Nanoseconds(), 400)

			return EdgeNodeRes{}, xerr.BadRequest("edge: request payload cannot be empty")
		}

		latency := time.Since(start).Nanoseconds()
		ring.Push(rawID, latency, 200)

		return EdgeNodeRes{
			Region:     region,
			NodeID:     nodeID,
			LatencyMs:  latency / 1_000_000,
			Data:       req,
			StatusCode: 200,
		}, nil
	}).
		Description(fmt.Sprintf("Location-transparent edge node for region %s", region)).
		Tag("edge", string(region)).
		Build()
}
