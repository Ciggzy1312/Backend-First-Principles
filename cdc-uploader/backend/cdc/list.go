package cdc

import (
	"log"
	"net/http"
	"time"

	"github.com/Ciggzy1312/cdc-uploader/respond"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func (h *Handler) listFiles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	cur, err := h.files.Aggregate(ctx, []bson.M{
		{"$sort": bson.M{"createdAt": -1}},
		{"$project": bson.M{
			"name":       1,
			"size":       1,
			"version":    1,
			"lastUpload": 1,
			"updatedAt":  1,
			"chunkCount": bson.M{"$size": "$chunks"},
		}},
	})
	if err != nil {
		log.Printf("cdc list: aggregate: %v", err)
		respond.Error(w, http.StatusInternalServerError, "could not list files")
		return
	}

	var rows []struct {
		ID         bson.ObjectID `bson:"_id"`
		Name       string        `bson:"name"`
		Size       int64         `bson:"size"`
		Version    int           `bson:"version"`
		ChunkCount int           `bson:"chunkCount"`
		Last       *LastUpload   `bson:"lastUpload"`
		UpdatedAt  time.Time     `bson:"updatedAt"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		log.Printf("cdc list: read: %v", err)
		respond.Error(w, http.StatusInternalServerError, "could not list files")
		return
	}

	views := make([]fileView, 0, len(rows)) // [] not null when empty
	for _, f := range rows {
		views = append(views, fileView{
			ID:         f.ID.Hex(),
			Name:       f.Name,
			Size:       f.Size,
			Version:    f.Version,
			ChunkCount: f.ChunkCount,
			LastUpload: f.Last,
			UpdatedAt:  f.UpdatedAt,
		})
	}
	respond.JSON(w, http.StatusOK, map[string]any{"files": views})
}
