package cdc

import (
	"errors"
	"log"
	"net/http"

	"github.com/Ciggzy1312/cdc-uploader/respond"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func (h *Handler) deleteFile(w http.ResponseWriter, r *http.Request) {
	id, err := bson.ObjectIDFromHex(r.PathValue("id"))
	if err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid file id")
		return
	}

	var file File
	err = h.files.FindOneAndDelete(r.Context(), bson.M{"_id": id}).Decode(&file)
	if errors.Is(err, mongo.ErrNoDocuments) {
		respond.Error(w, http.StatusNotFound, "file not found")
		return
	}
	if err != nil {
		log.Printf("cdc remove %s: %v", id.Hex(), err)
		respond.Error(w, http.StatusInternalServerError, "could not delete the file")
		return
	}

	if err := h.release(distinctHashes(file.Chunks)); err != nil {
		log.Printf("cdc remove %s: release references: %v", id.Hex(), err)
	}
	respond.NoContent(w)
}
