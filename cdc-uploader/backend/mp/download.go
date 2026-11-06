package mp

import (
	"log"
	"mime"
	"net/http"

	"github.com/Ciggzy1312/cdc-uploader/respond"
)

func (h *Handler) download(w http.ResponseWriter, r *http.Request) {
	file, ok := h.findFile(w, r)
	if !ok {
		return
	}

	if file.Status != StatusReady || file.S3Key == "" {
		respond.Error(w, http.StatusConflict, "file is not ready for download")
		return
	}

	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": file.Name})
	if disposition == "" {
		disposition = "attachment"
	}
	url, err := h.s3.PresignGetObject(r.Context(), file.S3Key, disposition)
	if err != nil {
		log.Printf("download: %v", err)
		respond.Error(w, http.StatusInternalServerError, "could not create download link")
		return
	}

	http.Redirect(w, r, url, http.StatusFound)
}
