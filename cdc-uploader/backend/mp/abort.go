package mp

import (
	"log"
	"net/http"
	"time"

	"github.com/Ciggzy1312/cdc-uploader/respond"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// abort handles POST /api/mp/files/{id}/abort.
//
// It cancels an upload that has started but not completed: the browser gave
// up, a part failed for good, or the user closed the tab and we are cleaning
// up later. Two cases:
//
//   - a brand-new file (nothing stored yet): the whole record is deleted
//   - an update to an existing file (v1 is still in place): only the pending
//     upload is dropped, and the file keeps its current version
//
// Order: S3 first, Mongo second. S3 abort is safe to repeat (see
// store.AbortMultipart), so if Mongo fails after S3 succeeded, the client
// simply retries and finishes the cleanup.
func (h *Handler) abort(w http.ResponseWriter, r *http.Request) {
	file, ok := h.findFile(w, r)
	if !ok {
		return
	}
	if file.Pending == nil {
		respond.Error(w, http.StatusConflict, "file has no upload in progress")
		return
	}
	ctx := r.Context()

	if err := h.s3.AbortMultipart(ctx, file.Pending.S3Key, file.Pending.UploadID); err != nil {
		log.Printf("abort: %v", err)
		respond.Error(w, http.StatusBadGateway, "storage could not cancel the upload")
		return
	}

	var err error
	if file.S3Key == "" {
		_, err = h.files.DeleteOne(ctx, bson.M{"_id": file.ID, "s3Key": ""})
	} else {
		_, err = h.files.UpdateOne(ctx, bson.M{"_id": file.ID}, bson.M{
			"$unset": bson.M{"pending": ""},
			"$set":   bson.M{"updatedAt": time.Now().UTC()},
		})
	}
	if err != nil {
		log.Printf("abort: mongo: %v", err)
		respond.Error(w, http.StatusInternalServerError, "upload cancelled but record update failed; retry")
		return
	}

	respond.NoContent(w)
}
