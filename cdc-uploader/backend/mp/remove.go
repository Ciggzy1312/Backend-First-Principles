package mp

import (
	"log"
	"net/http"

	"github.com/Ciggzy1312/cdc-uploader/respond"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	file, ok := h.findFile(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	if file.Pending != nil {
		if err := h.s3.AbortMultipart(ctx, file.Pending.S3Key, file.Pending.UploadID); err != nil {
			log.Printf("remove: %v", err)
			respond.Error(w, http.StatusBadGateway, "storage could not cancel the pending upload")
			return
		}
	}

	if file.S3Key != "" {
		if err := h.s3.DeleteObject(ctx, file.S3Key); err != nil {
			log.Printf("remove: %v", err)
			respond.Error(w, http.StatusBadGateway, "storage could not delete the file")
			return
		}
	}

	if _, err := h.files.DeleteOne(ctx, bson.M{"_id": file.ID}); err != nil {
		log.Printf("remove: mongo delete: %v", err)
		respond.Error(w, http.StatusInternalServerError, "file data deleted but record removal failed; retry")
		return
	}

	respond.NoContent(w)
}
