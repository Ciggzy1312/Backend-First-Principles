package mp

import (
	"errors"
	"log"
	"net/http"

	"github.com/Ciggzy1312/cdc-uploader/respond"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func (h *Handler) findFile(w http.ResponseWriter, r *http.Request) (file File, ok bool) {
	id, err := bson.ObjectIDFromHex(r.PathValue("id"))
	if err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid file id")
		return File{}, false
	}

	err = h.files.FindOne(r.Context(), bson.M{"_id": id}).Decode(&file)
	if errors.Is(err, mongo.ErrNoDocuments) {
		respond.Error(w, http.StatusNotFound, "file not found")
		return File{}, false
	}
	if err != nil {
		log.Printf("findFile: mongo find: %v", err)
		respond.Error(w, http.StatusInternalServerError, "could not load file")
		return File{}, false
	}
	return file, true
}
