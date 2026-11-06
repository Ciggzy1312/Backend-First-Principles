// The upload itself: slice, hash, ask the server for URLs, PUT the parts to S3,
// then complete. This file has no React in it, so the flow reads top to bottom.

import {
  abortUpload,
  completeUpload,
  initiateUpload,
  type PartEtag,
} from "./api";
import { sha256Hex } from "./lib/sha256";

const PART_SIZE = 8 * 1024 * 1024; // 8 MiB. S3 requires at least 5 MiB per part except the last.
const PARALLEL_UPLOADS = 4; // how many parts are in flight at once
const MAX_ATTEMPTS = 3; // tries per part before giving up

export type UploadStatus =
  | { phase: "idle" }
  | { phase: "hashing"; done: number; total: number } // parts hashed so far
  | { phase: "uploading"; bytes: number; total: number } // bytes uploaded so far
  | { phase: "completing" }
  | { phase: "error"; message: string };

export async function uploadFile(
  file: File,
  onStatus: (status: UploadStatus) => void,
): Promise<void> {
  if (file.size === 0) throw new Error("Empty files are not supported");
  const started = performance.now();

  // 1. Cut the file into parts and hash each one. file.slice() does not copy
  //    any data; a Blob slice is just a window onto the file.
  const count = Math.ceil(file.size / PART_SIZE);
  const blobs: Blob[] = [];
  const hashes: string[] = [];
  for (let i = 0; i < count; i++) {
    onStatus({ phase: "hashing", done: i, total: count });
    const blob = file.slice(i * PART_SIZE, Math.min((i + 1) * PART_SIZE, file.size));
    blobs.push(blob);
    hashes.push(await sha256Hex(blob));
  }

  // 2. Initiate: the server answers with one presigned S3 URL per part.
  const { fileId, parts } = await initiateUpload({
    name: file.name,
    size: file.size,
    partSize: PART_SIZE,
    partHashes: hashes,
  });

  try {
    // 3. PUT each part straight to S3. S3 answers each with an ETag, a receipt
    //    that identifies the exact bytes it stored.
    const etags: PartEtag[] = [];
    let uploaded = 0;
    onStatus({ phase: "uploading", bytes: 0, total: file.size });
    await runPool(parts, PARALLEL_UPLOADS, async (part) => {
      const blob = blobs[part.n - 1];
      etags.push({ n: part.n, etag: await putPart(part.url, blob) });
      uploaded += blob.size;
      onStatus({ phase: "uploading", bytes: uploaded, total: file.size });
    });

    // 4. Complete: send the receipts so S3 can assemble the parts into one file.
    onStatus({ phase: "completing" });
    await completeUpload(fileId, etags, Math.round(performance.now() - started));
  } catch (err) {
    // Do not leave a half-finished upload (and its stored parts) behind.
    await abortUpload(fileId).catch(() => {});
    throw err;
  }
}

// PUT one part to its presigned URL, retrying a few times, and return its ETag.
async function putPart(url: string, blob: Blob): Promise<string> {
  let lastError: unknown;
  for (let attempt = 1; attempt <= MAX_ATTEMPTS; attempt++) {
    try {
      const res = await fetch(url, { method: "PUT", body: blob });
      if (!res.ok) throw new Error(`S3 rejected a part (HTTP ${res.status})`);
      const etag = res.headers.get("ETag");
      if (!etag) {
        throw new Error(
          "S3 did not expose the ETag header; check the bucket's CORS settings",
        );
      }
      return etag;
    } catch (err) {
      lastError = err;
    }
  }
  throw lastError;
}

// Run fn over items with at most `limit` running at the same time. After the
// first failure no new items are started.
async function runPool<T>(
  items: T[],
  limit: number,
  fn: (item: T) => Promise<void>,
): Promise<void> {
  let next = 0;
  let failed = false;
  const worker = async () => {
    while (!failed && next < items.length) {
      const item = items[next++];
      try {
        await fn(item);
      } catch (err) {
        failed = true;
        throw err;
      }
    }
  };
  await Promise.all(Array.from({ length: Math.min(limit, items.length) }, worker));
}