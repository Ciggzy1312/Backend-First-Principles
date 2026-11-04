# Dropbox-style Sync POC: Multipart vs CDC Design

Nov 4, 2026 · @Deek

A two-uploader POC that shows why Dropbox-style systems chunk by content: updating a file with the first uploader re-sends every byte, while the second re-sends only the chunks that changed.

## Overview

The UI has two uploaders, each with its own file list beneath it. Both support upload, download and update (replace a file's contents with a newer version). Nothing here is production-grade: no auth, no multi-user, no resumable-after-crash, no encryption. The only goal is to learn and to measure bytes sent.

|  | Uploader 1: Multipart | Uploader 2: CDC |
| --- | --- | --- |
| Chunking | Fixed-size parts (8 MiB) | Content-defined (FastCDC-sty le, \~64 KiB avg) |
| S3 layout | One object per file | One object per chunk |
| On update | Rehash all parts, re-upload whole file | Rechunk, upload only chunks the server lacks |
| Dedup | None | Across versions and across files |
| Download | Presigned GET of the object | Server streams chunks in manifest order |

### Decisions already made

- **Stack:** Go API server, MongoDB for metadata, MinIO (S3-compatible) for objects, React frontend.
- **Where chunking runs:** in the browser for both uploaders, so the savings are real network savings.
- **CDC algorithm:** hand-written Gear-hash FastCDC in TypeScript, no library.
- **Versioning:** latest version only. An update replaces the file; nothing keeps history.
- **Run mode:** MinIO and MongoDB in docker compose; Go server and React dev server run locally.

### Non-goals

Auth, quotas, resumable uploads, directory trees, sharing, encryption, delta compression inside chunks, and multi-client sync or conflict resolution.

## Architecture and stack

The browser does the chunking and hashing; the Go server brokers uploads, verifies chunk integrity and owns all metadata.

&#91;embedded content: system architecture · browser, Go server, MongoDB, MinIO\]

Uploader 1 talks to MinIO directly using presigned URLs. Uploader 2 sends chunks through the Go server so it can verify each hash before storing.

| Layer | Choice | Notes |
| --- | --- | --- |
| Frontend | React, Vite, TypeScript | Web Crypto for SHA-256; hand-written CDC in `lib/fastcdc.ts` |
| API | Go 1.22, `net/http` | Pattern router, no framework |
| Metadata | MongoDB 7 | `mp_files`, `cdc_files`, `cdc_chunks` |
| Object store | MinIO | S3 API, path-style addressing, one bucket `dropbox-poc` |
| Runtime | docker compose for MinIO and MongoDB | Go server and React dev server run on the host |

## Data model (MongoDB)

Three collections, one per concept. Uploader 1 needs one; Uploader 2 needs two because chunks are shared objects with a reference count.

### `mp_files` (Uploader 1)

```json
{
  "_id": "ObjectId",
  "name": "report.pdf",
  "size": 52428800,
  "status": "ready",            // uploading | ready
  "version": 2,
  "s3Key": "mp/<id>/v2",         // current object
  "partSize": 8388608,
  "parts": [ { "n": 1, "size": 8388608, "sha256": "ab12...", "etag": "..." } ],
  "pending": { "s3Key": "mp/<id>/v3", "uploadId": "..." },  // only during upload/update
  "lastUpload": { "bytesSent": 52428800, "durationMs": 4100 },
  "createdAt": "...", "updatedAt": "..."
}
```

Part hashes are stored but never used to skip uploads. That is the point: after an update you can compare old and new hashes in the UI and see how many parts were identical yet still re-sent.

### `cdc_files` (Uploader 2)

```json
{
  "_id": "ObjectId",
  "name": "report.pdf",
  "size": 52428800,
  "version": 2,
  "chunks": [ { "hash": "ab12...", "size": 61234 }, { "hash": "cd34...", "size": 70112 } ],
  "fileHash": "sha256 of whole file",
  "lastUpload": { "chunksTotal": 820, "chunksSent": 3, "bytesSent": 191344 },
  "createdAt": "...", "updatedAt": "..."
}
```

The ordered `chunks` array is the manifest. Offsets are implied by cumulative sizes.

### `cdc_chunks`

```json
{ "_id": "ab12...",   // sha256 hex
  "size": 61234,
  "s3Key": "chunks/ab/ab12...",
  "refCount": 2,
  "createdAt": "..." }
```

`refCount` is the number of manifests slots that point at the chunk, counting duplicates inside one file. Chunk identity is the SHA-256 of its bytes, so the same chunk in two files or two versions is stored once.

### S3 (MinIO) layout

| Bucket prefix | Contents | Written by |
| --- | --- | --- |
| `mp/<fileId>/v<n>` | Whole file as one multipart object | Uploader 1 |
| `chunks/<hash[0:2]>/<hash>` | One CDC chunk | Uploader 2 |

## Uploader 1: whole-file multipart

Every upload and every update sends the full file to S3 as a multipart upload. The browser sends parts straight to MinIO using presigned URLs; the Go server only brokers the upload and records metadata.

### Upload (new file)

1. **Browser:** slice the file into 8 MiB parts (S3 requires 5 MiB minimum except the last) and compute SHA-256 of each part with Web Crypto.
2. **Browser → Go:** `POST /api/mp/files` with `{name, size, partSize, partHashes[]}`.
3. **Go → S3:** `CreateMultipartUpload` on key `mp/<id>/v1`. Insert `mp_files` with `status: uploading` and `pending.uploadId`.
4. **Go → Browser:** return `fileId` and one presigned `UploadPart` URL per part.
5. **Browser → S3:** `PUT` each part to its URL, 3 to 4 in parallel. Read each `ETag` response header.
6. **Browser → Go:** `POST /api/mp/files/{id}/complete` with `[{n, etag}]`.
7. **Go → S3:** `CompleteMultipartUpload`. Update Mongo: `status: ready`, `s3Key`, `parts`, clear `pending`.

### Update

Same seven steps, started with `POST /api/mp/files/{id}/update`. The client always rechunks and rehashes the whole new file, then uploads every part to a new key `mp/<id>/v<n+1>`. On complete, the server swaps `s3Key`, bumps `version`, and deletes the old object. The new object is written before the old one is removed, so a failed update never loses the file.

After completion the UI compares new part hashes against the stored old ones and shows: "X of Y parts were identical but were re-uploaded anyway". Because parts are fixed-size, inserting one byte near the start shifts every later part boundary and usually makes all hashes differ. That is the weakness Uploader 2 fixes.

### Download

`GET /api/mp/files/{id}/download` returns a 302 to a presigned `GetObject` URL with `Content-Disposition: attachment`. The browser downloads directly from MinIO.

### Failure handling (kept minimal)

- Abandoned `uploading` records stay in the list marked as incomplete. A "cleanup" button calls `AbortMultipartUpload` and deletes the record.
- A failed part `PUT` is retried up to 3 times; after that the whole upload fails and nothing is committed.

## Uploader 2: content-defined chunking

The browser cuts the file at content-determined boundaries, hashes each chunk, asks the server which hashes it lacks, uploads only those, then commits a manifest. Upload and update are the same flow; the only difference is whether a file record already exists.

### Upload and update

1. **Browser:** run CDC over the file (next section) to get chunks. Hash each with SHA-256. Build the manifest `[{hash, size}]` and a whole-file hash.
2. **Browser → Go:** `POST /api/cdc/chunks/missing` with the unique hashes. Go checks `cdc_chunks` and returns the ones not present.
3. **Browser → Go:** for each missing chunk, `PUT /api/cdc/chunks/{hash}` with the raw bytes, 4 to 6 in parallel.
4. **Go:** recompute SHA-256 of the body and reject on mismatch. `PutObject` to `chunks/<hash[0:2]>/<hash>`, then upsert `cdc_chunks` with `refCount: 0`.
5. **Browser → Go:** commit. New file: `POST /api/cdc/files` with `{name, size, fileHash, chunks[]}`. Update: `PUT /api/cdc/files/{id}` with the same body.
6. **Go:** verify every manifest hash exists in `cdc_chunks`. For a new file, `$inc` refCount by 1 per manifest slot. For an update, compute the diff between old and new manifests and `$inc` only the differences (+1 for added slots, -1 for removed slots). Write the new manifest and bump `version`.
7. **Go (GC):** delete chunks whose `refCount` reached 0: remove the S3 object, then the Mongo doc.

The UI shows `chunksSent / chunksTotal` and `bytesSent` after each operation. Edit a few bytes in the middle of a large file and update: you should see a handful of chunks sent instead of the whole file.

### Why chunk uploads go through the Go server

Uploader 1 uses presigned URLs to show how multipart works. Here chunks are small and the server must verify each chunk's hash before trusting it as a dedup key, so the bytes pass through Go. It also keeps the S3 object and the `cdc_chunks` row consistent in one place.

### Download

`GET /api/cdc/files/{id}/download` sets `Content-Length` and `Content-Disposition`, then loops over the manifest: `GetObject` for each chunk in order and copy to the response. Optionally verify each chunk's hash while streaming. Sequential fetch is fine for a POC; prefetching the next chunk is a stretch goal.

### Failure handling (kept minimal)

- Chunks uploaded before a failed commit have `refCount: 0` and are orphans. A "GC" button deletes all chunks with `refCount: 0` older than a few minutes.
- Commit is rejected if any chunk is missing; the client re-runs the missing-check and retries.

## Chunking algorithm (browser, TypeScript)

A chunk boundary is declared wherever a rolling hash of the last few bytes matches a pattern, so boundaries depend on content rather than position. Insert bytes early in a file and only the chunks around the edit change; later boundaries land on the same content as before.

### Parameters

| Parameter | Value | Why |
| --- | --- | --- |
| Min chunk | 16 KiB | Avoids tiny chunks and S3 object explosion |
| Average chunk | 64 KiB | Small enough that a small edit re-sends little; large enough to keep object count sane in a demo |
| Max chunk | 256 KiB | Forces a cut when no boundary is found |
| Hash for identity | SHA-256 | `crypto.subtle.digest`, same on client and server |

A 100 MiB file at 64 KiB average is about 1,600 S3 objects. That is fine for a POC and makes the per-object overhead a visible lesson. Real systems use larger averages (hundreds of KiB to a few MiB).

### Gear-hash cut (FastCDC style)

```ts
// 256 random 32-bit values, generated once from a fixed seed so chunking is deterministic
const GEAR: Uint32Array = buildGearTable(0x5eed);

const MIN = 16 * 1024, AVG = 64 * 1024, MAX = 256 * 1024;
// FastCDC "normalized chunking": stricter mask before AVG, looser after,
// which tightens the chunk size distribution around AVG.
const MASK_S = makeMask(17);  // more 1 bits: match chance ~1/2^17, harder
const MASK_L = makeMask(15);  // fewer 1 bits: match chance ~1/2^15, easier

function nextCut(buf: Uint8Array, start: number): number {
  const end = Math.min(buf.length, start + MAX);
  if (end - start <= MIN) return end;
  let fp = 0;
  let i = start + MIN;                // skip bytes below the minimum size
  const normal = Math.min(end, start + AVG);
  for (; i < normal; i++) {
    fp = ((fp << 1) + GEAR[buf[i]]) >>> 0;
    if ((fp & MASK_S) === 0) return i + 1;
  }
  for (; i < end; i++) {
    fp = ((fp << 1) + GEAR[buf[i]]) >>> 0;
    if ((fp & MASK_L) === 0) return i + 1;
  }
  return end;                         // hit MAX or end of file
}
```

`makeMask(n)` returns a 32-bit mask with n bits set, spread across the word (the original FastCDC uses 64-bit masks; in JS keep a 32-bit fingerprint and spread the bits). Verify empirically on random data that the average chunk size lands near 64 KiB and tune the two bit counts if not.

### Reading large files

Do not load the whole file into memory. Read the file in 4 to 8 MiB windows with `file.slice().arrayBuffer()`, run `nextCut` over the window, carry the unfinished tail into the next window, and hash each finished chunk immediately. Keep only `{hash, size}` plus the chunk's `Blob` slice reference for later upload.

## API

JSON over HTTP under `/api`. No auth. CORS enabled for the React dev origin.

### Uploader 1 (multipart)

| Method and path | Request | Response |
| --- | --- | --- |
| `GET /api/mp/files` | none | List of files (name, size, version, status, lastUpload) |
| `POST /api/mp/files` | `{name, size, partSize, partHashes[]}` | `{fileId, parts: [{n, url}]}` |
| `POST /api/mp/files/{id}/update` | `{name?, size, partSize, partHashes[]}` | `{parts: [{n, url}]}` with a new pending key |
| `POST /api/mp/files/{id}/complete` | `{parts: [{n, etag}], durationMs}` | `{file}` |
| `POST /api/mp/files/{id}/abort` | none | 204; aborts pending upload |
| `GET /api/mp/files/{id}/download` | none | 302 to presigned GET |
| `DELETE /api/mp/files/{id}` | none | 204; deletes object and record |

### Uploader 2 (CDC)

| Method and path | Request | Response |
| --- | --- | --- |
| `GET /api/cdc/files` | none | List of files (name, size, version, chunk count, lastUpload) |
| `POST /api/cdc/chunks/missing` | `{hashes[]}` | `{missing: [hash]}` |
| `PUT /api/cdc/chunks/{hash}` | raw bytes | 204; 400 if body hash differs |
| `POST /api/cdc/files` | `{name, size, fileHash, chunks[], stats}` | `{file}` |
| `PUT /api/cdc/files/{id}` | `{size, fileHash, chunks[], stats}` | `{file}` |
| `GET /api/cdc/files/{id}/download` | none | Streamed file body |
| `DELETE /api/cdc/files/{id}` | none | 204; decrements refCounts, GC |
| `POST /api/cdc/gc` | none | `{deleted}`; removes `refCount: 0` chunks older than 5 minutes |

### Notes

- `stats` is client-reported (`chunksTotal`, `chunksSent`, `bytesSent`) and only used for display.
- Presigned URL TTL: 15 minutes.
- Errors use `{error: string}` with a 4xx or 5xx status.

## UI, project layout and local setup

### UI

One page, two panels side by side. Each panel has a file picker, an upload progress bar, and the list of its files below.

- **Each list row:** name, size, version, last-upload stats, and buttons for Download, Update (opens a file picker bound to that row) and Delete.
- **Uploader 1 stats:** bytes sent, duration, and "X of Y parts identical but re-sent" after an update.
- **Uploader 2 stats:** chunks total, chunks sent, bytes sent, and a savings percentage after an update.
- **Uploader 2 extra:** a "Run GC" button and a count of chunks stored.

No routing, no state library. React with `useState` and `fetch` is enough.

### Repository layout

```
dropbox-poc/
  docker-compose.yml        # minio + mongo
  server/                   # Go
    main.go
    config.go               # env: MONGO_URI, S3_ENDPOINT, S3_BUCKET, keys
    store/
      mongo.go              # collections and indexes
      s3.go                 # aws-sdk-go-v2 client, path-style, presign helpers
    mp/handlers.go          # Uploader 1 endpoints
    cdc/handlers.go         # Uploader 2 endpoints
    cdc/gc.go               # refCount diff and cleanup
  web/                      # React + Vite + TypeScript
    src/
      api.ts
      MultipartUploader.tsx
      CdcUploader.tsx
      lib/fastcdc.ts        # chunker
      lib/sha256.ts         # Web Crypto wrapper
```

### Go libraries

- `net/http` with the Go 1.22 pattern router (no framework needed)
- `go.mongodb.org/mongo-driver/v2`
- `github.com/aws/aws-sdk-go-v2` with `s3` and presign, endpoint set to MinIO and path-style addressing on

### docker compose (MinIO and MongoDB only)

```yaml
services:
  mongo:
    image: mongo:7
    ports: ["27017:27017"]
    volumes: ["mongo-data:/data/db"]
  minio:
    image: minio/minio
    command: server /data --console-address ":9001"
    environment:
      MINIO_ROOT_USER: minioadmin
      MINIO_ROOT_PASSWORD: minioadmin
    ports: ["9000:9000", "9001:9001"]
    volumes: ["minio-data:/data"]
  minio-init:
    image: minio/mc
    depends_on: [minio]
    entrypoint: >
      /bin/sh -c "mc alias set local http://minio:9000 minioadmin minioadmin &&
      mc mb -p local/dropbox-poc"
volumes:
  mongo-data:
  minio-data:
```

### Setup gotchas to check early

- **Browser PUT to MinIO needs CORS:** MinIO allows all origins by default, but confirm that the response exposes the `ETag` header, since the browser must read it to complete the multipart upload.
- **Presigned host:** the Go server must presign against the same host the browser will call (`localhost:9000`), not an internal Docker hostname.
- **Mongo indexes:** none needed beyond `_id`; `cdc_chunks._id` is the hash.

## Build plan, experiments and edge cases

Build Uploader 1 first because it is the baseline, then Uploader 2 so the comparison is meaningful.

### Milestones

- [ ] 1\. docker compose up, Go server connects to Mongo and MinIO, health endpoint
- [ ] 2\. Uploader 1: create, complete, list, download
- [ ] 3\. Uploader 1: update flow with swap and old-object delete, identical-parts stat
- [ ] 4\. `fastcdc.ts` chunker, checked by eye on a sample file
- [ ] 5\. Uploader 2: missing-check, chunk PUT with hash verify, commit, list
- [ ] 6\. Uploader 2: streaming download, update with refCount diff, delete and GC
- [ ] 7\. Run the experiments below and note the numbers

### Experiments to run

| Experiment | Uploader 1 expected | Uploader 2 expected |
| --- | --- | --- |
| Upload a 50 MiB random file | 50 MiB sent | About 50 MiB sent |
| Update: change 10 bytes in the middle | 50 MiB sent | A few chunks, under 1 MiB |
| Update: insert 10 bytes at the start | 50 MiB sent, and nearly every part hash differs | A few chunks, under 1 MiB |
| Upload the same file under a new name | 50 MiB sent | 0 bytes sent (full dedup) |
| Append 5 MiB to the end | 55 MiB sent | About 5 MiB sent |

The third row is the key one: it is where fixed-size parts break down and content-defined chunks do not.

### Edge cases to decide while building

- **Empty file:** reject for both uploaders to avoid zero-part multipart uploads and empty manifests.
- **File smaller than min chunk:** CDC returns a single chunk; works without special-casing.
- **Duplicate chunks inside one file:** manifest lists the hash twice, upload once, `refCount` counts both slots.
- **Concurrent updates to the same file:** out of scope. Last commit wins.
- **Chunk uploaded but commit never arrives:** orphan with `refCount: 0`, collected by the GC button.
- **GC racing with an in-flight upload:** the 5 minute age guard makes this unlikely; ignore it in the POC.

### Stretch ideas, only if time allows

- Compare chunk-size settings (16/64/256 KiB average) on the same edit and plot chunks sent.
- Compress chunks before storing.
- Keep the last N manifests per file to get version history for free, since old chunks are still deduplicated.
