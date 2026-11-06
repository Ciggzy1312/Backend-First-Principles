package mp

import (
	"log"
	"net/http"

	"github.com/Ciggzy1312/cdc-uploader/respond"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// list handles GET /api/mp/files.
//
// It returns every file, newest first. That includes files still in the
// "uploading" state (an upload in flight, or one that was abandoned), so the UI
// can show them as incomplete instead of hiding them.
//
// A file's "parts" array can hold up to 10,000 entries (one per 8 MiB part) and
// the list screen never needs it, so the query projects it away.
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	cur, err := h.files.Find(ctx, bson.M{},
		options.Find().
			SetSort(bson.D{{Key: "createdAt", Value: -1}}).
			SetProjection(bson.M{"parts": 0}))
	if err != nil {
		log.Printf("list: mongo find: %v", err)
		respond.Error(w, http.StatusInternalServerError, "could not list files")
		return
	}

	var files []File
	if err := cur.All(ctx, &files); err != nil {
		log.Printf("list: mongo read: %v", err)
		respond.Error(w, http.StatusInternalServerError, "could not list files")
		return
	}

	views := make([]fileView, 0, len(files))
	for _, f := range files {
		views = append(views, toView(f))
	}
	respond.JSON(w, http.StatusOK, map[string]any{"files": views})
}
