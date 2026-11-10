package cdc

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/Ciggzy1312/cdc-uploader/respond"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	DefaultGCGrace = 10 * time.Minute

	gcBatch = 1000 // chunks handled per run; run again if "more" is true
)

// parseGrace reads the optional ?grace=30s query value.
func parseGrace(v string) (time.Duration, error) {
	if v == "" {
		return DefaultGCGrace, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("grace must be a duration like 30s or 10m")
	}
	return d, nil
}

func (h *Handler) runGC(w http.ResponseWriter, r *http.Request) {
	grace, err := parseGrace(r.URL.Query().Get("grace"))
	if err != nil {
		respond.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	cutoff := time.Now().Add(-grace)
	garbage := bson.M{"refCount": 0, "zeroSince": bson.M{"$lt": cutoff}}

	cur, err := h.chunks.Find(ctx, garbage,
		options.Find().SetLimit(gcBatch).SetProjection(bson.M{"_id": 1, "size": 1}))
	if err != nil {
		log.Printf("cdc gc: find: %v", err)
		respond.Error(w, http.StatusInternalServerError, "gc failed")
		return
	}
	var candidates []struct {
		Hash string `bson:"_id"`
		Size int64  `bson:"size"`
	}
	if err := cur.All(ctx, &candidates); err != nil {
		log.Printf("cdc gc: read: %v", err)
		respond.Error(w, http.StatusInternalServerError, "gc failed")
		return
	}

	var deleted, skipped, failed int
	var bytesFreed int64
	for _, c := range candidates {
		filter := bson.M{"_id": c.Hash, "refCount": 0, "zeroSince": bson.M{"$lt": cutoff}}
		res, err := h.chunks.DeleteOne(ctx, filter)
		if err != nil {
			log.Printf("cdc gc: delete record %s: %v", c.Hash, err)
			failed++
			continue
		}
		if res.DeletedCount == 0 {
			skipped++
			continue
		}

		if err := h.s3.DeleteObject(ctx, ChunkKey(c.Hash)); err != nil {
			log.Printf("cdc gc: delete object %s: %v", c.Hash, err)
			failed++
			continue
		}
		deleted++
		bytesFreed += c.Size

		if _, err := h.chunks.DeleteOne(ctx, bson.M{"_id": c.Hash, "refCount": 0}); err != nil {
			log.Printf("cdc gc: cleanup %s: %v", c.Hash, err)
		}
	}

	respond.JSON(w, http.StatusOK, map[string]any{
		"deleted":    deleted,
		"bytesFreed": bytesFreed,
		"skipped":    skipped,
		"failed":     failed,
		"more":       len(candidates) == gcBatch,
	})
}
