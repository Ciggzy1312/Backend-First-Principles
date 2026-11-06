package mp

import (
	"fmt"
	"strings"
)

const (
	MinPartSize = 5 * 1024 * 1024
	MaxParts    = 10000
	maxNameLen  = 255
)

type InitiateRequest struct {
	Name       string   `json:"name"`
	Size       int64    `json:"size"`
	PartSize   int64    `json:"partSize"`
	PartHashes []string `json:"partHashes"`
}

type Part struct {
	N      int    `json:"n" bson:"n"`
	Size   int64  `json:"size" bson:"size"`
	SHA256 string `json:"sha256" bson:"sha256"`
}

func ValidateParts(req InitiateRequest) ([]Part, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if len(name) > maxNameLen {
		return nil, fmt.Errorf("name is longer than %d characters", maxNameLen)
	}
	if req.Size <= 0 {
		return nil, fmt.Errorf("size must be greater than 0 (empty files are not supported)")
	}
	if req.PartSize < MinPartSize {
		return nil, fmt.Errorf("partSize must be at least %d bytes (5 MiB)", MinPartSize)
	}

	// Ceiling division: 20 MiB at 8 MiB parts -> 3 parts (8 + 8 + 4).
	count := (req.Size-1)/req.PartSize + 1
	if count > MaxParts {
		return nil, fmt.Errorf("file needs %d parts but S3 allows at most %d; use a larger partSize", count, MaxParts)
	}
	if int64(len(req.PartHashes)) != count {
		return nil, fmt.Errorf("size %d with partSize %d needs %d partHashes, got %d",
			req.Size, req.PartSize, count, len(req.PartHashes))
	}

	parts := make([]Part, count)
	for i := range parts {
		h := strings.ToLower(req.PartHashes[i])
		if !isSHA256Hex(h) {
			return nil, fmt.Errorf("partHashes[%d] is not a 64-character hex SHA-256", i)
		}
		size := req.PartSize
		if i == len(parts)-1 {
			size = req.Size - int64(i)*req.PartSize
		}
		parts[i] = Part{N: i + 1, Size: size, SHA256: h}
	}
	return parts, nil
}

func isSHA256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
