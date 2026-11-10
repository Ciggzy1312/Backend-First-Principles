package cdc

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"strconv"

	"github.com/Ciggzy1312/cdc-uploader/respond"
)

func (h *Handler) downloadFile(w http.ResponseWriter, r *http.Request) {
	file, ok := h.findFile(w, r)
	if !ok {
		return
	}

	for i, ref := range file.Chunks {
		data, err := h.fetchChunk(r, ref)
		if err != nil {
			log.Printf("cdc download %s: chunk %d (%s): %v", file.ID.Hex(), i, ref.Hash, err)
			if i == 0 {
				// Nothing sent yet, so we can still answer with a proper error.
				respond.Error(w, http.StatusInternalServerError, "could not read the file's data")
				return
			}
			// Headers and some bytes are already out; the status cannot change.
			// Cutting the connection makes the download FAIL. Because we promised
			// a Content-Length, the browser sees fewer bytes than promised and
			// reports an error, instead of saving a truncated file as complete.
			panic(http.ErrAbortHandler)
		}

		if i == 0 {
			// Headers go out only once the first chunk is safely in hand.
			disposition := mime.FormatMediaType("attachment", map[string]string{"filename": file.Name})
			if disposition == "" {
				disposition = "attachment"
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", disposition)
			w.Header().Set("Content-Length", strconv.FormatInt(file.Size, 10))
		}
		if _, err := w.Write(data); err != nil {
			return
		}
	}
}

func (h *Handler) fetchChunk(r *http.Request, ref ChunkRef) ([]byte, error) {
	body, err := h.s3.GetObject(r.Context(), ChunkKey(ref.Hash))
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return verifyChunk(body, ref)
}

func verifyChunk(body io.Reader, ref ChunkRef) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, int64(ref.Size)+1))
	if err != nil {
		return nil, err
	}
	if len(data) != ref.Size {
		return nil, fmt.Errorf("size is %d, manifest says %d", len(data), ref.Size)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != ref.Hash {
		return nil, fmt.Errorf("content does not match its hash")
	}
	return data, nil
}
