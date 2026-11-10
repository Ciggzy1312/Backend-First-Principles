package cdc

import "time"

type fileView struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Size       int64       `json:"size"`
	Version    int         `json:"version"`
	ChunkCount int         `json:"chunkCount"`
	LastUpload *LastUpload `json:"lastUpload,omitempty"`
	UpdatedAt  time.Time   `json:"updatedAt"`
}

func toView(f File) fileView {
	return fileView{
		ID:         f.ID.Hex(),
		Name:       f.Name,
		Size:       f.Size,
		Version:    f.Version,
		ChunkCount: len(f.Chunks),
		LastUpload: f.Last,
		UpdatedAt:  f.UpdatedAt,
	}
}
