package cdc

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/Ciggzy1312/cdc-uploader/respond"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func (h *Handler) updateFile(w http.ResponseWriter, r *http.Request) {
	old, ok := h.findFile(w, r)
	if !ok {
		return
	}

	var req CreateRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Name == "" {
		req.Name = old.Name
	}
	if err := req.validate(); err != nil {
		respond.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	added, removed := diffHashes(old.Chunks, req.Chunks)

	if len(added) > 0 {
		matched, err := h.acquire(r.Context(), added)
		if err != nil {
			log.Printf("cdc update %s: acquire: %v", old.ID.Hex(), err)
			respond.Error(w, http.StatusInternalServerError, "could not update the file")
			return
		}
		if matched != int64(len(added)) {
			if err := h.release(added); err != nil {
				log.Printf("cdc update %s: release after shortfall: %v", old.ID.Hex(), err)
			}
			h.replyMissing(w, r, added)
			return
		}
	}

	now := time.Now()
	res, err := h.files.UpdateOne(r.Context(),
		bson.M{"_id": old.ID, "version": old.Version},
		bson.M{"$set": bson.M{
			"name":      req.Name,
			"size":      req.Size,
			"chunks":    req.Chunks,
			"version":   old.Version + 1,
			"updatedAt": now,
			"lastUpload": LastUpload{
				ChunksTotal: len(req.Chunks),
				ChunksSent:  req.ChunksSent,
				BytesSent:   req.BytesSent,
				DurationMs:  req.DurationMs,
			},
		}})
	if err == nil && res.MatchedCount == 0 {
		if len(added) > 0 {
			if rerr := h.release(added); rerr != nil {
				log.Printf("cdc update %s: release after conflict: %v", old.ID.Hex(), rerr)
			}
		}
		respond.Error(w, http.StatusConflict, "the file was changed or deleted by someone else; reload and try again")
		return
	}
	if err != nil {
		log.Printf("cdc update %s: swap: %v", old.ID.Hex(), err)
		if len(added) > 0 {
			if rerr := h.release(added); rerr != nil {
				log.Printf("cdc update %s: release after failed swap: %v", old.ID.Hex(), rerr)
			}
		}
		respond.Error(w, http.StatusInternalServerError, "could not update the file")
		return
	}

	if len(removed) > 0 {
		if err := h.release(removed); err != nil {
			log.Printf("cdc update %s: release removed: %v", old.ID.Hex(), err)
		}
	}

	updated := old
	updated.Name = req.Name
	updated.Size = req.Size
	updated.Chunks = req.Chunks
	updated.Version = old.Version + 1
	updated.UpdatedAt = now
	updated.Last = &LastUpload{
		ChunksTotal: len(req.Chunks),
		ChunksSent:  req.ChunksSent,
		BytesSent:   req.BytesSent,
		DurationMs:  req.DurationMs,
	}
	respond.JSON(w, http.StatusOK, map[string]any{"file": toView(updated)})
}
