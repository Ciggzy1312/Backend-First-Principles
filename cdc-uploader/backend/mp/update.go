package mp

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/Ciggzy1312/cdc-uploader/respond"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	file, ok := h.findFile(w, r)
	if !ok {
		return
	}
	if file.Status != StatusReady || file.S3Key == "" {
		respond.Error(w, http.StatusConflict, "file is not ready to be updated")
		return
	}
	if file.Pending != nil {
		respond.Error(w, http.StatusConflict, "an upload is already in progress for this file")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, REQ_BODY_LIMIT)
	var req InitiateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		req.Name = file.Name
	}

	parts, err := ValidateParts(req)
	if err != nil {
		respond.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()
	version := file.Version + 1
	key := fmt.Sprintf("mp/%s/v%d", file.ID.Hex(), version)

	uploadID, urls, err := h.startUpload(ctx, key, parts)
	if err != nil {
		log.Printf("update: %v", err)
		respond.Error(w, http.StatusInternalServerError, "could not start upload")
		return
	}

	res, err := h.files.UpdateOne(ctx,
		bson.M{"_id": file.ID, "status": StatusReady, "pending": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{
			"pending": Pending{
				S3Key:    key,
				UploadID: uploadID,
				Version:  version,
				Name:     req.Name,
				Size:     req.Size,
				PartSize: req.PartSize,
				Parts:    parts,
			},
			"updatedAt": time.Now().UTC(),
		}})
	if err != nil {
		log.Printf("update: mongo: %v", err)
		h.abortQuietly(key, uploadID)
		respond.Error(w, http.StatusInternalServerError, "could not save update")
		return
	}
	if res.MatchedCount == 0 {
		h.abortQuietly(key, uploadID)
		respond.Error(w, http.StatusConflict, "another upload started for this file first")
		return
	}

	respond.JSON(w, http.StatusOK, initiateResponse{FileID: file.ID.Hex(), Parts: urls})
}
