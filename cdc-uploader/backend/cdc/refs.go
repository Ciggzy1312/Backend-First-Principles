package cdc

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func distinctHashes(refs []ChunkRef) []string {
	out := make([]string, 0, len(refs))
	seen := make(map[string]bool, len(refs))
	for _, r := range refs {
		if !seen[r.Hash] {
			seen[r.Hash] = true
			out = append(out, r.Hash)
		}
	}
	return out
}

func (h *Handler) acquire(ctx context.Context, hashes []string) (int64, error) {
	res, err := h.chunks.UpdateMany(ctx,
		bson.M{"_id": bson.M{"$in": hashes}},
		bson.M{
			"$inc":   bson.M{"refCount": 1},
			"$unset": bson.M{"zeroSince": ""},
		})
	if err != nil {
		return 0, err
	}
	return res.MatchedCount, nil
}

func (h *Handler) release(hashes []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := h.chunks.UpdateMany(ctx,
		bson.M{"_id": bson.M{"$in": hashes}},
		bson.M{"$inc": bson.M{"refCount": -1}})
	if err != nil {
		return err
	}
	_, err = h.chunks.UpdateMany(ctx,
		bson.M{"_id": bson.M{"$in": hashes}, "refCount": 0},
		bson.M{"$set": bson.M{"zeroSince": time.Now()}})
	return err
}
