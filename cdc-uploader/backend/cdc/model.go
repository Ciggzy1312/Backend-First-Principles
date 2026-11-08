package cdc

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

//	cdc_chunks  one document per distinct chunk of content (shared by all files)
//	cdc_files   one document per file: its name and the ordered list of chunks

type Chunk struct {
	Hash      string     `bson:"_id"`
	Size      int        `bson:"size"`
	RefCount  int        `bson:"refCount"` // how many files list this chunk; 0 = garbage
	CreatedAt time.Time  `bson:"createdAt"`
	ZeroSince *time.Time `bson:"zeroSince,omitempty"`
}

// ChunkRef is one entry in a file's manifest: which chunk, and how big.
type ChunkRef struct {
	Hash string `bson:"hash" json:"hash"`
	Size int    `bson:"size" json:"size"`
}

// File is one document in cdc_files.
type File struct {
	ID        bson.ObjectID `bson:"_id"`
	Name      string        `bson:"name"`
	Size      int64         `bson:"size"`
	Version   int           `bson:"version"`
	Chunks    []ChunkRef    `bson:"chunks"`
	Last      *LastUpload   `bson:"lastUpload,omitempty"`
	CreatedAt time.Time     `bson:"createdAt"`
	UpdatedAt time.Time     `bson:"updatedAt"`
}

// LastUpload records what the most recent upload or update really sent, which
// is the number this whole uploader exists to show.
type LastUpload struct {
	ChunksTotal int   `bson:"chunksTotal" json:"chunksTotal"`
	ChunksSent  int   `bson:"chunksSent" json:"chunksSent"` // chunks the server did not have yet
	BytesSent   int64 `bson:"bytesSent" json:"bytesSent"`
	DurationMs  int64 `bson:"durationMs" json:"durationMs"`
}

// Collection names.
const (
	ChunksCollection = "cdc_chunks"
	FilesCollection  = "cdc_files"
)

// ChunkKey is where a chunk's bytes live in S3. The key depends only on the
// content hash, so the same content always maps to the same object.
func ChunkKey(hash string) string {
	return "cdc/chunks/" + hash
}
