# cdc-uploader

A learning project: two ways to upload, download and **update** files in a Dropbox-style system, side by side, so you can see what content-defined chunking (CDC) saves.

| | Uploader 1: Multipart | Uploader 2: CDC |
|---|---|---|
| Cuts the file | fixed 8 MiB parts | content-defined chunks (about 64 KiB) |
| Update re-sends | the whole file | only chunks the server lacks |
| Insert bytes at the start | every part changes | about one chunk changes |
| Where bytes go | straight to S3 (presigned URLs) | through the Go server, one S3 object per chunk |
| Versions | latest only | latest only |

This is a POC for learning, not a product. It has no auth, no users, and a few known gaps (see the end).

## Stack

- **Backend:** Go 1.22, `net/http`, MongoDB (metadata), S3-compatible storage
- **Storage:** Silo (a drop-in MinIO fork) in Docker. The original `minio/minio` image was removed from Docker Hub.
- **Frontend:** React 19, TypeScript, Tailwind 4, Vite 7
- Only MongoDB and Silo run in Docker. The Go server and the frontend run on your machine.

## Run it

You need Docker, Go 1.22+ and Node 20.19+ (or 22.12+). Vite 7 refuses older Node.

```
make setup     # first time: start MongoDB + Silo, install Go and npm dependencies
make dev       # start MongoDB + Silo, then the API and the UI together
```

Then open http://localhost:5173. The Silo console is at http://localhost:9001 (login `minioadmin` / `minioadmin`).

`make help` lists everything:

| Command | What it does |
|---|---|
| `make infra-up` / `infra-down` | start / stop MongoDB and Silo (data kept in the containers) |
| `make infra-reset` | stop them and delete all stored data |
| `make server` | run the Go API on :8080 |
| `make web` | run the React dev server on :5173 |
| `make health` | check the API is up |
| `make mongo-shell` | open a Mongo shell on the project database |
| `make infra-logs` / `make status` | follow logs / show container state |

### Settings

The server reads environment variables, all with local defaults:

| Variable | Default |
|---|---|
| `ADDR` | `:8080` |
| `MONGO_URI` / `MONGO_DB` | `mongodb://localhost:27017` / `dropbox_poc` |
| `S3_ENDPOINT` | `http://localhost:9000` |
| `S3_REGION` / `S3_BUCKET` | `us-east-1` / `dropbox-poc` |
| `S3_ACCESS_KEY` / `S3_SECRET_KEY` | `minioadmin` / `minioadmin` |

The server creates the bucket on startup. The browser origin allowed by CORS (`http://localhost:5173`) is set in `backend/main.go`. The frontend reads `VITE_API_URL` (default `http://localhost:8080`).

## Layout

```
docker-compose.yml     MongoDB + Silo
Makefile               the commands above
scripts/               mp-initiate.sh: try Uploader 1's first call from the shell
backend/
  main.go, config.go   wiring, settings, CORS
  store/               Mongo and S3 clients (presign, multipart, put/get object)
  respond/             JSON response helpers
  mp/                  Uploader 1 routes (one file per route)
  cdc/                 Uploader 2 routes, models, reference counting, GC
frontend/src/
  App.tsx              the Uploader 1 / Uploader 2 toggle
  MultipartUploader.tsx, multipart.ts   Uploader 1 UI and upload flow
  CdcUploader.tsx, cdc.ts, fastcdc.ts   Uploader 2 UI, upload flow, chunker
  api.ts, ui.tsx, lib/ server calls and shared pieces
```

## Uploader 1: multipart

1. The browser cuts the file into 8 MiB parts and hashes each (SHA-256).
2. `POST /api/mp/files` sends the name, size and part hashes. The server starts an S3 multipart upload and returns one presigned URL per part.
3. The browser `PUT`s the parts straight to S3 (4 at a time, 3 tries each) and keeps each part's `ETag`.
4. `POST /api/mp/files/{id}/complete` sends the ETags, and S3 assembles the object.

| Route | Purpose |
|---|---|
| `GET /api/mp/files` | list files |
| `POST /api/mp/files` | start an upload |
| `POST /api/mp/files/{id}/update` | start an update (the old version stays downloadable) |
| `POST /api/mp/files/{id}/complete` | finish an upload or update |
| `POST /api/mp/files/{id}/abort` | cancel an unfinished upload or update |
| `GET /api/mp/files/{id}/download` | 302 redirect to a presigned S3 URL |
| `DELETE /api/mp/files/{id}` | delete the file and its object |

An update writes to a new S3 key (`mp/<id>/v<n+1>`) and swaps over only on complete, then deletes the old object. After an update the list shows how many parts were identical to the old version yet re-sent anyway.

## Uploader 2: content-defined chunking

1. The browser cuts the file with a hand-written Gear/FastCDC chunker (min 16 KiB, average 64 KiB, max 256 KiB) and hashes each chunk.
2. `POST /api/cdc/chunks/missing` sends the hashes. The server replies with the ones it lacks.
3. The browser `PUT`s only those chunks to `/api/cdc/chunks/{hash}`. The server re-hashes the bytes, refuses a mismatch, and stores each chunk as its own S3 object (`cdc/chunks/<hash>`).
4. `POST /api/cdc/files` commits the file as an ordered list of chunk hashes.

| Route | Purpose |
|---|---|
| `GET /api/cdc/files` | list files |
| `POST /api/cdc/chunks/missing` | which of these hashes are missing? |
| `PUT /api/cdc/chunks/{hash}` | store one chunk (verified) |
| `POST /api/cdc/files` | commit a new file |
| `PUT /api/cdc/files/{id}` | replace a file's chunk list |
| `GET /api/cdc/files/{id}/download` | stream the chunks in order, each re-verified |
| `DELETE /api/cdc/files/{id}` | delete the file, release its chunks |
| `POST /api/cdc/gc[?grace=10m]` | delete chunks nobody uses |

Two Mongo collections: `cdc_chunks` (`_id` is the SHA-256, plus `refCount` and `zeroSince`) and `cdc_files` (name, size, version, and the ordered chunk list).

### Rules worth knowing

- **`refCount` counts files, not occurrences.** A file that uses the same chunk five times adds 1. Commit, update and delete all work on the distinct hashes.
- **A file exists only once it is committed.** Half-finished uploads leave unreferenced chunks, never a broken file.
- **Write order is chosen so a crash wastes space but never loses data.** On commit, references are taken before the file is inserted. On delete, the file is removed before references are released. On GC, the Mongo record goes before the S3 object.
- **GC only deletes a chunk if it is still at `refCount` 0 and has been unused longer than the grace period** (10 minutes by default). The check is part of the delete itself, so it cannot race with a commit claiming the chunk.
- **Updates swap the chunk list only if the file is still at the version they read**, so a stale update cannot overwrite a newer one.

## Experiments

Run the same edit on both uploaders and compare "bytes sent":

| Edit | Expected |
|---|---|
| Change 10 bytes in the middle | Uploader 1 re-sends everything; Uploader 2 sends about one chunk |
| Insert bytes at the start | Uploader 1: no part matches; Uploader 2: about one chunk |
| Upload the same file under a new name | Uploader 1 sends everything again; Uploader 2 sends nothing |
| Append 5 MiB | both send about the new data |

After deleting or updating files in Uploader 2, click "Clean up unused chunks" and watch `cdc/chunks/` shrink in the Silo console.

## Known gaps (on purpose)

- A crash between S3 completing and the Mongo write in Uploader 1's complete step leaves them out of sync.
- GC has one small race: re-uploading the exact same chunk in the instant between GC deleting its record and its object. GC cleans up after itself, but a file committed in that same instant would break.
- S3 objects with no Mongo record (left by a crash) are never found or removed.
- Uploader 2 has no server-side "uploading" state: cancelling or crashing just leaves unreferenced chunks for GC.
- No auth, no limits per user, one version of each file.