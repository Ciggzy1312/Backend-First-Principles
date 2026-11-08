package cdc

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/Ciggzy1312/cdc-uploader/respond"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	MaxChunkSize = 256 * 1024 // the chunker's maximum; anything bigger is refused
	maxHashes    = 200_000    // per missing-chunks request (about 12 GiB of 64 KiB chunks)
	maxBodyBytes = 16 << 20   // 16 MiB cap on the missing-chunks JSON body
)

func isHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func (h *Handler) missingChunks(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Hashes []string `json:"hashes"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(req.Hashes) > maxHashes {
		respond.Error(w, http.StatusBadRequest, "too many hashes in one request")
		return
	}

	unique := make([]string, 0, len(req.Hashes))
	seen := make(map[string]bool, len(req.Hashes))
	for _, hash := range req.Hashes {
		if !isHash(hash) {
			respond.Error(w, http.StatusBadRequest, "every hash must be a lowercase hex SHA-256")
			return
		}
		if !seen[hash] {
			seen[hash] = true
			unique = append(unique, hash)
		}
	}

	have := map[string]bool{}
	if len(unique) > 0 {
		cur, err := h.chunks.Find(r.Context(),
			bson.M{"_id": bson.M{"$in": unique}},
			options.Find().SetProjection(bson.M{"_id": 1}))
		if err != nil {
			log.Printf("cdc missing: find: %v", err)
			respond.Error(w, http.StatusInternalServerError, "could not look up chunks")
			return
		}
		var found []struct {
			Hash string `bson:"_id"`
		}
		if err := cur.All(r.Context(), &found); err != nil {
			log.Printf("cdc missing: read: %v", err)
			respond.Error(w, http.StatusInternalServerError, "could not look up chunks")
			return
		}
		for _, c := range found {
			have[c.Hash] = true
		}

		// We are about to tell the browser "you don't need to send these"
		now := time.Now()
		_, err = h.chunks.UpdateMany(r.Context(),
			bson.M{"_id": bson.M{"$in": unique}, "refCount": 0},
			bson.M{"$set": bson.M{"zeroSince": now}})
		if err != nil {
			log.Printf("cdc missing: touch: %v", err)
			respond.Error(w, http.StatusInternalServerError, "could not look up chunks")
			return
		}
	}

	missing := make([]string, 0)
	for _, hash := range unique {
		if !have[hash] {
			missing = append(missing, hash)
		}
	}
	respond.JSON(w, http.StatusOK, map[string]any{"missing": missing})
}

func (h *Handler) putChunk(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if !isHash(hash) {
		respond.Error(w, http.StatusBadRequest, "hash must be a lowercase hex SHA-256")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, MaxChunkSize)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			respond.Error(w, http.StatusRequestEntityTooLarge, "chunk is larger than the 256 KiB maximum")
			return
		}
		respond.Error(w, http.StatusBadRequest, "could not read the chunk body")
		return
	}
	if len(data) == 0 {
		respond.Error(w, http.StatusBadRequest, "chunk is empty")
		return
	}

	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != hash {
		respond.Error(w, http.StatusBadRequest, "chunk content does not match its hash")
		return
	}

	if err := h.s3.PutObject(r.Context(), ChunkKey(hash), data); err != nil {
		log.Printf("cdc put chunk %s: %v", hash, err)
		respond.Error(w, http.StatusBadGateway, "could not store the chunk")
		return
	}

	// $setOnInsert handles race condition, overwrites the second value so it does not break
	now := time.Now()
	_, err = h.chunks.UpdateOne(r.Context(),
		bson.M{"_id": hash},
		bson.M{"$setOnInsert": bson.M{
			"size":      len(data),
			"refCount":  0,
			"zeroSince": now,
			"createdAt": now,
		}},
		options.UpdateOne().SetUpsert(true))
	if err != nil {
		log.Printf("cdc put chunk %s: mongo: %v", hash, err)
		respond.Error(w, http.StatusInternalServerError, "could not record the chunk")
		return
	}
	respond.NoContent(w)
}
