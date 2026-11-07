package mp

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

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

func (h *Handler) startUpload(ctx context.Context, key string, parts []Part) (uploadID string, urls []partURL, err error) {
	uploadID, err = h.s3.CreateMultipart(ctx, key)
	if err != nil {
		return "", nil, err
	}

	urls = make([]partURL, len(parts))
	for i, p := range parts {
		u, err := h.s3.PresignUploadPart(ctx, key, uploadID, p.N)
		if err != nil {
			h.abortQuietly(key, uploadID)
			return "", nil, err
		}
		urls[i] = partURL{N: p.N, URL: u}
	}
	return uploadID, urls, nil
}

func (h *Handler) deleteQuietly(key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.s3.DeleteObject(ctx, key); err != nil {
		log.Printf("cleanup: %v (object %s left behind)", err, key)
	}
}

func (h *Handler) abortQuietly(key, uploadID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.s3.AbortMultipart(ctx, key, uploadID); err != nil {
		log.Printf("cleanup: %v (upload %s may need manual abort)", err, uploadID)
	}
}
