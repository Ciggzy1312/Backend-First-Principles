package mp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/Ciggzy1312/cdc-uploader/respond"
	"go.mongodb.org/mongo-driver/v2/bson"
)

const REQ_BODY_LIMIT = 2 * 1024 * 1024 // 2 MiB (enough for 10000 partHashes of 64-67 bytes each, plus JSON overhead)

const (
	StatusUploading = "uploading"
	StatusReady     = "ready"
)

type File struct {
	ID        bson.ObjectID `bson:"_id"`
	Name      string        `bson:"name"`
	Size      int64         `bson:"size"`
	Status    string        `bson:"status"`
	Version   int           `bson:"version"`
	S3Key     string        `bson:"s3Key"`
	PartSize  int64         `bson:"partSize"`
	Parts     []Part        `bson:"parts"`
	Pending   *Pending      `bson:"pending,omitempty"`
	Last      *LastUpload   `bson:"lastUpload,omitempty"`
	CreatedAt time.Time     `bson:"createdAt"`
	UpdatedAt time.Time     `bson:"updatedAt"`
}

// LastUpload records stats about the most recent upload, shown in the UI.
type LastUpload struct {
	BytesSent  int64 `bson:"bytesSent" json:"bytesSent"`
	DurationMs int64 `bson:"durationMs" json:"durationMs"`
}

// Pending remembers the in-flight multipart upload so we can finish or abort it.
type Pending struct {
	S3Key    string `bson:"s3Key"`
	UploadID string `bson:"uploadId"`
}

type partURL struct {
	N   int    `json:"n"`
	URL string `json:"url"`
}

type initiateResponse struct {
	FileID string    `json:"fileId"`
	Parts  []partURL `json:"parts"`
}

func (h *Handler) initiate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, REQ_BODY_LIMIT)

	var req InitiateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}

	parts, err := ValidateParts(req)
	if err != nil {
		respond.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()
	id := bson.NewObjectID()
	key := fmt.Sprintf("mp/%s/v1", id.Hex())

	uploadID, err := h.s3.CreateMultipart(ctx, key)
	if err != nil {
		log.Printf("initiate: %v", err)
		respond.Error(w, http.StatusInternalServerError, "could not start upload")
		return
	}

	urls := make([]partURL, len(parts))
	for i, p := range parts {
		u, err := h.s3.PresignUploadPart(ctx, key, uploadID, p.N)
		if err != nil {
			log.Printf("initiate: %v", err)
			h.abortQuietly(key, uploadID)
			respond.Error(w, http.StatusInternalServerError, "could not start upload")
			return
		}
		urls[i] = partURL{N: p.N, URL: u}
	}

	// 4. Mongo insert
	now := time.Now().UTC()
	doc := File{
		ID:        id,
		Name:      req.Name,
		Size:      req.Size,
		Status:    StatusUploading,
		Version:   1,
		PartSize:  req.PartSize,
		Parts:     parts,
		Pending:   &Pending{S3Key: key, UploadID: uploadID},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, err := h.files.InsertOne(ctx, doc); err != nil {
		log.Printf("initiate: mongo insert: %v", err)
		h.abortQuietly(key, uploadID)
		respond.Error(w, http.StatusInternalServerError, "could not save file record")
		return
	}

	respond.JSON(w, http.StatusCreated, initiateResponse{FileID: id.Hex(), Parts: urls})
}

func (h *Handler) abortQuietly(key, uploadID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.s3.AbortMultipart(ctx, key, uploadID); err != nil {
		log.Printf("cleanup: %v (upload %s may need manual abort)", err, uploadID)
	}
}
