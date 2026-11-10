package cdc

import (
	"net/http"

	"github.com/Ciggzy1312/cdc-uploader/respond"
	"github.com/Ciggzy1312/cdc-uploader/store"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Handler serves the Uploader 2 routes.
type Handler struct {
	chunks *mongo.Collection // cdc_chunks
	files  *mongo.Collection // cdc_files
	s3     *store.S3
}

func NewHandler(chunks, files *mongo.Collection, s3 *store.S3) *Handler {
	return &Handler{files: files, s3: s3}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/cdc/files", h.listFiles)
	mux.HandleFunc("POST /api/cdc/chunks/missing", h.missingChunks)
	mux.HandleFunc("PUT /api/cdc/chunks/{hash}", h.putChunk)
	mux.HandleFunc("POST /api/cdc/files", h.createFile)
	mux.HandleFunc("PUT /api/cdc/files/{id}", h.updateFile)
	mux.HandleFunc("GET /api/cdc/files/{id}/download", h.downloadFile)
	mux.HandleFunc("DELETE /api/cdc/files/{id}", h.deleteFile)
	mux.HandleFunc("POST /api/cdc/gc", h.runGC)
}

func dummyFile(id string) map[string]any {
	return map[string]any{
		"id":         id,
		"name":       "report.pdf",
		"size":       52428800,
		"version":    2,
		"chunkCount": 820,
		"lastUpload": map[string]any{
			"chunksTotal": 820,
			"chunksSent":  3,
			"bytesSent":   191344,
		},
	}
}

// PUT /api/cdc/files/{id}   body: {size, fileHash, chunks[], stats}
func updateFile(w http.ResponseWriter, r *http.Request) {
	respond.JSON(w, http.StatusOK, map[string]any{
		"file": dummyFile(r.PathValue("id")),
	})
}

// DELETE /api/cdc/files/{id}
func deleteFile(w http.ResponseWriter, r *http.Request) {
	respond.NoContent(w)
}

// POST /api/cdc/gc
func runGC(w http.ResponseWriter, r *http.Request) {
	respond.JSON(w, http.StatusOK, map[string]any{"deleted": 0})
}
