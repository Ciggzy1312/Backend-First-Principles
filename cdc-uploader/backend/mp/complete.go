package mp

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/Ciggzy1312/cdc-uploader/respond"
	"github.com/Ciggzy1312/cdc-uploader/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type CompleteRequest struct {
	Parts      []PartETag `json:"parts"`
	DurationMs int64      `json:"durationMs"`
}

type fileView struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Size       int64       `json:"size"`
	Version    int         `json:"version"`
	Status     string      `json:"status"`
	LastUpload *LastUpload `json:"lastUpload,omitempty"`
	UpdatedAt  time.Time   `json:"updatedAt"`
}

func toView(f File) fileView {
	return fileView{
		ID:         f.ID.Hex(),
		Name:       f.Name,
		Size:       f.Size,
		Version:    f.Version,
		Status:     f.Status,
		LastUpload: f.Last,
		UpdatedAt:  f.UpdatedAt,
	}
}

func (h *Handler) complete(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, REQ_BODY_LIMIT)
	var req CompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}

	ctx := r.Context()

	file, ok := h.findFile(w, r)
	if !ok {
		return
	}

	if file.Status != StatusUploading || file.Pending == nil {
		respond.Error(w, http.StatusConflict, "file has no upload in progress")
		return
	}

	ordered, err := OrderParts(len(file.Parts), req.Parts)
	if err != nil {
		respond.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	completed := make([]store.CompletedPart, len(ordered))
	for i, p := range ordered {
		completed[i] = store.CompletedPart{N: p.N, ETag: p.ETag}
	}

	if err := h.s3.CompleteMultipart(ctx, file.Pending.S3Key, file.Pending.UploadID, completed); err != nil {
		log.Printf("complete: %v", err)
		respond.Error(w, http.StatusBadGateway, "storage could not assemble the file; check that every part was uploaded")
		return
	}

	now := time.Now().UTC()
	last := LastUpload{BytesSent: file.Size, DurationMs: req.DurationMs}
	res, err := h.files.UpdateOne(ctx,
		bson.M{"_id": file.ID, "status": StatusUploading},
		bson.M{
			"$set": bson.M{
				"status":     StatusReady,
				"s3Key":      file.Pending.S3Key,
				"lastUpload": last,
				"updatedAt":  now,
			},
			"$unset": bson.M{"pending": ""},
		})
	if err != nil {
		log.Printf("complete: mongo update: %v", err)
		respond.Error(w, http.StatusInternalServerError, "upload assembled but record update failed")
		return
	}
	if res.MatchedCount == 0 {
		respond.Error(w, http.StatusConflict, "file was already completed")
		return
	}

	file.Status = StatusReady
	file.S3Key = file.Pending.S3Key
	file.Last = &last
	respond.JSON(w, http.StatusOK, map[string]any{"file": toView(file)})
}
