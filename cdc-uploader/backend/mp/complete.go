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
	Updating   bool        `json:"updating"` // flag used to check pending stuck updates and show cancel button in UI
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
		Updating:   f.Pending != nil && f.S3Key != "",
	}
}

type uploadTarget struct {
	Version  int
	Name     string
	Size     int64
	PartSize int64
	Parts    []Part
}

func targetOf(f File) uploadTarget {
	if f.S3Key == "" {
		return uploadTarget{f.Version, f.Name, f.Size, f.PartSize, f.Parts}
	}
	p := f.Pending
	return uploadTarget{p.Version, p.Name, p.Size, p.PartSize, p.Parts}
}

func (h *Handler) complete(w http.ResponseWriter, r *http.Request) {
	file, ok := h.findFile(w, r)
	if !ok {
		return
	}
	if file.Pending == nil {
		respond.Error(w, http.StatusConflict, "file has no upload in progress")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, REQ_BODY_LIMIT)
	var req CompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}

	ctx := r.Context()
	isUpdate := file.S3Key != ""
	target := targetOf(file)

	ordered, err := OrderParts(len(target.Parts), req.Parts)
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
	last := LastUpload{BytesSent: target.Size, DurationMs: req.DurationMs}
	set := bson.M{
		"status":    StatusReady,
		"s3Key":     file.Pending.S3Key,
		"updatedAt": now,
	}

	if isUpdate {
		set["version"] = target.Version
		set["name"] = target.Name
		set["size"] = target.Size
		set["partSize"] = target.PartSize
		set["parts"] = target.Parts
		last.Update = &UpdateStats{
			PartsTotal:     len(target.Parts),
			PartsIdentical: CountIdentical(file.Parts, target.Parts),
		}
	}
	set["lastUpload"] = last

	res, err := h.files.UpdateOne(ctx,
		bson.M{"_id": file.ID, "pending.uploadId": file.Pending.UploadID},
		bson.M{"$set": set, "$unset": bson.M{"pending": ""}})
	if err != nil {
		log.Printf("complete: mongo update: %v", err)
		respond.Error(w, http.StatusInternalServerError, "upload assembled but record update failed")
		return
	}
	if res.MatchedCount == 0 {
		respond.Error(w, http.StatusConflict, "upload was already completed")
		return
	}
	if isUpdate {
		h.deleteQuietly(file.S3Key)
	}

	file.Status = StatusReady
	file.S3Key = file.Pending.S3Key
	file.Pending = nil
	file.UpdatedAt = now
	if isUpdate {
		file.Version = target.Version
		file.Name = target.Name
		file.Size = target.Size
	}
	file.Last = &last
	respond.JSON(w, http.StatusOK, map[string]any{"file": toView(file)})
}
