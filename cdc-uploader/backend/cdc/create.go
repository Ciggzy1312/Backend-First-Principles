package cdc

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/Ciggzy1312/cdc-uploader/respond"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const maxNameLen = 255

type CreateRequest struct {
	Name       string     `json:"name"`
	Size       int64      `json:"size"`
	Chunks     []ChunkRef `json:"chunks"`
	ChunksSent int        `json:"chunksSent"`
	BytesSent  int64      `json:"bytesSent"`
	DurationMs int64      `json:"durationMs"`
}

func (r *CreateRequest) validate() error {
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" {
		return fmt.Errorf("name is required")
	}
	if len(r.Name) > maxNameLen {
		return fmt.Errorf("name is longer than %d characters", maxNameLen)
	}
	if len(r.Chunks) == 0 {
		return fmt.Errorf("a file needs at least one chunk")
	}
	if len(r.Chunks) > maxHashes {
		return fmt.Errorf("too many chunks")
	}
	var total int64
	for i, c := range r.Chunks {
		if !isHash(c.Hash) {
			return fmt.Errorf("chunk %d: hash must be a lowercase hex SHA-256", i)
		}
		if c.Size < 1 || c.Size > MaxChunkSize {
			return fmt.Errorf("chunk %d: size must be between 1 and %d bytes", i, MaxChunkSize)
		}
		total += int64(c.Size)
	}
	if total != r.Size {
		return fmt.Errorf("size (%d) does not equal the sum of the chunk sizes (%d)", r.Size, total)
	}
	if r.ChunksSent < 0 || r.ChunksSent > len(r.Chunks) || r.BytesSent < 0 || r.DurationMs < 0 {
		return fmt.Errorf("invalid upload stats")
	}
	return nil
}

// POST /api/cdc/files
//
// The commit. By now the browser has uploaded every chunk the server lacked, so
// all chunks in the list exist. This call records the file: its name and the
// ordered chunk list (the manifest). Until it succeeds there is no file, only
// unreferenced chunks that GC will eventually clean up.
//
// Order matters, because a crash can land between any two steps:
//
//  1. take a reference on every chunk  (refCount +1)
//  2. insert the file document
//
// If we crash after 1, some chunks hold a reference no file owns: they leak
// (never collected), which wastes space but loses nothing. The opposite order
// would risk a file whose chunks are not protected, and GC could delete them
// out from under it. Wasting space beats losing data.
func (h *Handler) createFile(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := req.validate(); err != nil {
		respond.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	// 1. Take a reference on each distinct chunk.
	hashes := distinctHashes(req.Chunks)
	matched, err := h.acquire(r.Context(), hashes)
	if err != nil {
		log.Printf("cdc create: acquire: %v", err)
		respond.Error(w, http.StatusInternalServerError, "could not record the file")
		return
	}
	if matched != int64(len(hashes)) {
		if err := h.release(hashes); err != nil {
			log.Printf("cdc create: release after shortfall: %v", err)
		}
		h.replyMissing(w, r, hashes)
		return
	}

	now := time.Now()
	doc := File{
		ID:      bson.NewObjectID(),
		Name:    req.Name,
		Size:    req.Size,
		Version: 1,
		Chunks:  req.Chunks,
		Last: &LastUpload{
			ChunksTotal: len(req.Chunks),
			ChunksSent:  req.ChunksSent,
			BytesSent:   req.BytesSent,
			DurationMs:  req.DurationMs,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, err := h.files.InsertOne(r.Context(), doc); err != nil {
		log.Printf("cdc create: insert: %v", err)
		if err := h.release(hashes); err != nil {
			log.Printf("cdc create: release after failed insert: %v", err)
		}
		respond.Error(w, http.StatusInternalServerError, "could not record the file")
		return
	}

	respond.JSON(w, http.StatusCreated, map[string]any{"file": toView(doc)})
}

func (h *Handler) replyMissing(w http.ResponseWriter, r *http.Request, hashes []string) {
	cur, err := h.chunks.Find(r.Context(),
		bson.M{"_id": bson.M{"$in": hashes}},
		options.Find().SetProjection(bson.M{"_id": 1}))
	var found []struct {
		Hash string `bson:"_id"`
	}
	if err == nil {
		err = cur.All(r.Context(), &found)
	}
	if err != nil {
		respond.Error(w, http.StatusConflict, "some chunks have not been uploaded")
		return
	}
	have := make(map[string]bool, len(found))
	for _, c := range found {
		have[c.Hash] = true
	}
	missing := make([]string, 0)
	for _, hash := range hashes {
		if !have[hash] {
			missing = append(missing, hash)
		}
	}
	respond.JSON(w, http.StatusConflict, map[string]any{
		"error":   "some chunks have not been uploaded",
		"missing": missing,
	})
}
