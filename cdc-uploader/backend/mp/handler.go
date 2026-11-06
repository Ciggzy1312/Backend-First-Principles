package mp

import (
	"net/http"

	"github.com/Ciggzy1312/cdc-uploader/respond"
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
	mux.HandleFunc("POST /api/mp/files/{id}/update", updateFile)
	mux.HandleFunc("POST /api/mp/files/{id}/complete", h.complete)
	mux.HandleFunc("POST /api/mp/files/{id}/abort", abortFile)
	mux.HandleFunc("GET /api/mp/files/{id}/download", h.download)
	mux.HandleFunc("DELETE /api/mp/files/{id}", deleteFile)
}

func dummyFile(id string) map[string]any {
	return map[string]any{
		"id":      id,
		"name":    "report.pdf",
		"size":    52428800,
		"version": 1,
		"status":  "ready",
		"lastUpload": map[string]any{
			"bytesSent":  52428800,
			"durationMs": 4100,
		},
	}
}

func dummyParts() []map[string]any {
	return []map[string]any{
		{"n": 1, "url": "http://localhost:9000/dropbox-poc/mp/dummy/v1?partNumber=1&uploadId=dummy"},
		{"n": 2, "url": "http://localhost:9000/dropbox-poc/mp/dummy/v1?partNumber=2&uploadId=dummy"},
		{"n": 3, "url": "http://localhost:9000/dropbox-poc/mp/dummy/v1?partNumber=3&uploadId=dummy"},
	}
}

// POST /api/mp/files/{id}/update   body: {name?, size, partSize, partHashes[]}
func updateFile(w http.ResponseWriter, r *http.Request) {
	respond.JSON(w, http.StatusOK, map[string]any{
		"fileId": r.PathValue("id"),
		"parts":  dummyParts(),
	})
}

// POST /api/mp/files/{id}/abort
func abortFile(w http.ResponseWriter, r *http.Request) {
	respond.NoContent(w)
}

// DELETE /api/mp/files/{id}
func deleteFile(w http.ResponseWriter, r *http.Request) {
	respond.NoContent(w)
}
