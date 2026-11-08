package cdc

import (
	"io"
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
	mux.HandleFunc("GET /api/cdc/files", listFiles)
	mux.HandleFunc("POST /api/cdc/chunks/missing", h.missingChunks)
	mux.HandleFunc("PUT /api/cdc/chunks/{hash}", h.putChunk)
	mux.HandleFunc("POST /api/cdc/files", createFile)
	mux.HandleFunc("PUT /api/cdc/files/{id}", updateFile)
	mux.HandleFunc("GET /api/cdc/files/{id}/download", downloadFile)
	mux.HandleFunc("DELETE /api/cdc/files/{id}", deleteFile)
	mux.HandleFunc("POST /api/cdc/gc", runGC)
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

// GET /api/cdc/files
func listFiles(w http.ResponseWriter, r *http.Request) {
	respond.JSON(w, http.StatusOK, map[string]any{
		"files": []map[string]any{dummyFile("dummy-1")},
	})
}

// POST /api/cdc/chunks/missing   body: {hashes[]}
func missingChunks(w http.ResponseWriter, r *http.Request) {
	respond.JSON(w, http.StatusOK, map[string]any{
		"missing": []string{"dummyhash1", "dummyhash2"},
	})
}

// PUT /api/cdc/chunks/{hash}   body: raw chunk bytes
func putChunk(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	respond.NoContent(w)
}

// POST /api/cdc/files   body: {name, size, fileHash, chunks[], stats}
func createFile(w http.ResponseWriter, r *http.Request) {
	respond.JSON(w, http.StatusCreated, map[string]any{
		"file": dummyFile("dummy-1"),
	})
}

// PUT /api/cdc/files/{id}   body: {size, fileHash, chunks[], stats}
func updateFile(w http.ResponseWriter, r *http.Request) {
	respond.JSON(w, http.StatusOK, map[string]any{
		"file": dummyFile(r.PathValue("id")),
	})
}

// GET /api/cdc/files/{id}/download
func downloadFile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="dummy.txt"`)
	_, _ = w.Write([]byte("dummy file contents\n"))
}

// DELETE /api/cdc/files/{id}
func deleteFile(w http.ResponseWriter, r *http.Request) {
	respond.NoContent(w)
}

// POST /api/cdc/gc
func runGC(w http.ResponseWriter, r *http.Request) {
	respond.JSON(w, http.StatusOK, map[string]any{"deleted": 0})
}
