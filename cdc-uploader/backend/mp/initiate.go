package mp

import (
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
	BytesSent  int64        `bson:"bytesSent" json:"bytesSent"`
	DurationMs int64        `bson:"durationMs" json:"durationMs"`
	Update     *UpdateStats `bson:"update,omitempty" json:"update,omitempty"` // only after an update
}

// UpdateStats compares an update with the version it replaced.
type UpdateStats struct {
	PartsTotal     int `bson:"partsTotal" json:"partsTotal"`
	PartsIdentical int `bson:"partsIdentical" json:"partsIdentical"`
}

// Pending remembers the in-flight multipart upload so we can finish or abort it.
type Pending struct {
	S3Key    string `bson:"s3Key"`
	UploadID string `bson:"uploadId"`
	// The fields below are set only for an UPDATE. They describe the new
	// version and replace the file's current details when the update completes;
	// until then the file keeps serving its old version. For a brand-new file
	// the details already sit on the file itself, so these stay empty.
	Version  int    `bson:"version,omitempty"`
	Name     string `bson:"name,omitempty"`
	Size     int64  `bson:"size,omitempty"`
	PartSize int64  `bson:"partSize,omitempty"`
	Parts    []Part `bson:"parts,omitempty"`
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

	uploadID, urls, err := h.startUpload(ctx, key, parts)
	if err != nil {
		log.Printf("update: %v", err)
		respond.Error(w, http.StatusInternalServerError, "could not start upload")
		return
	}

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
