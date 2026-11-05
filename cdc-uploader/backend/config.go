package main

import "os"

type Config struct {
	Addr string

	MongoURI string
	MongoDB  string

	S3Endpoint  string // must be reachable from the BROWSER too: presigned URLs embed it
	S3Region    string
	S3Bucket    string
	S3AccessKey string
	S3SecretKey string
}

func loadConfig() Config {
	return Config{
		Addr:        env("ADDR", ":8080"),
		MongoURI:    env("MONGO_URI", "mongodb://localhost:27017"),
		MongoDB:     env("MONGO_DB", "dropbox_poc"),
		S3Endpoint:  env("S3_ENDPOINT", "http://localhost:9000"),
		S3Region:    env("S3_REGION", "us-east-1"),
		S3Bucket:    env("S3_BUCKET", "dropbox-poc"),
		S3AccessKey: env("S3_ACCESS_KEY", "minioadmin"),
		S3SecretKey: env("S3_SECRET_KEY", "minioadmin"),
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
