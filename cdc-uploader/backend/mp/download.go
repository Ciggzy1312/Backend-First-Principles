package mp

import (
	"errors"
	"log"
	"mime"
	"net/http"

	"github.com/Ciggzy1312/cdc-uploader/respond"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func (h *Handler) download(w http.ResponseWriter, r *http.Request) {
	id, err := bson.ObjectIDFromHex(r.PathValue("id"))
	if err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid file id")
		return
	}

	var file File
	err = h.files.FindOne(r.Context(), bson.M{"_id": id}).Decode(&file)
	if errors.Is(err, mongo.ErrNoDocuments) {
		respond.Error(w, http.StatusNotFound, "file not found")
		return
	}
	if err != nil {
		log.Printf("download: mongo find: %v", err)
		respond.Error(w, http.StatusInternalServerError, "could not load file")
		return
	}

	if file.Status != StatusReady || file.S3Key == "" {
		respond.Error(w, http.StatusConflict, "file is not ready for download")
		return
	}

	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": file.Name})
	if disposition == "" {
		disposition = "attachment"
	}
	url, err := h.s3.PresignGetObject(r.Context(), file.S3Key, disposition)
	if err != nil {
		log.Printf("download: %v", err)
		respond.Error(w, http.StatusInternalServerError, "could not create download link")
		return
	}

	http.Redirect(w, r, url, http.StatusFound)
}
