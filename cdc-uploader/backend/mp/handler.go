package mp

import (
	"net/http"

	"github.com/Ciggzy1312/cdc-uploader/store"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

type Handler struct {
	files *mongo.Collection
	s3    *store.S3
}

func NewHandler(files *mongo.Collection, s3 *store.S3) *Handler {
	return &Handler{files: files, s3: s3}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/mp/files", h.list)
	mux.HandleFunc("POST /api/mp/files", h.initiate)
	mux.HandleFunc("POST /api/mp/files/{id}/update", h.update)
	mux.HandleFunc("POST /api/mp/files/{id}/complete", h.complete)
	mux.HandleFunc("POST /api/mp/files/{id}/abort", h.abort)
	mux.HandleFunc("GET /api/mp/files/{id}/download", h.download)
	mux.HandleFunc("DELETE /api/mp/files/{id}", h.remove)
}
