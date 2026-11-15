// The Uploader 2 upload: cut the file into content-defined chunks, ask the
// server which chunks it lacks, upload only those, then commit the file as an
// ordered list of chunk hashes. No React in this file, so the flow reads top to
// bottom.

import {
  ApiError,
  chunkUrl,
  createCdcFile,
  missingChunks,
  updateCdcFile,
  type ChunkRef,
} from "./api";
import { chunkFile, type Chunk } from "./fastcdc";
import { runPool } from "./lib/pool";

const PARALLEL_UPLOADS = 4; // chunks in flight at once
const MAX_ATTEMPTS = 3; // tries per chunk before giving up

export type CdcStatus =
  | { phase: "idle" }
  | { phase: "chunking"; done: number; total: number } // bytes cut and hashed so far
  | { phase: "checking" } // asking the server what it already has
  | { phase: "uploading"; chunks: number; totalChunks: number; bytes: number; totalBytes: number }
  | { phase: "committing" }
  | { phase: "error"; message: string };

type Options = {
  signal?: AbortSignal; // abort to cancel; chunks already uploaded are left for GC
  updateFileId?: string; // replace this file's content instead of adding a file
};

// Uploads a new file, or replaces the content of options.updateFileId. Either
// way the whole file is cut and hashed in the browser (cheap, local), but only
// chunks the server does not have are sent.
export async function uploadCdc(
  file: File,
  onStatus: (status: CdcStatus) => void,
  { signal, updateFileId }: Options = {},
): Promise<void> {
  if (file.size === 0) throw new Error("Empty files are not supported");
  const started = performance.now();

  // 1. Cut and hash. Only offsets, sizes and hashes are kept in memory; the
  //    bytes are read from the file again if a chunk has to be uploaded.
  const chunks: Chunk[] = [];
  onStatus({ phase: "chunking", done: 0, total: file.size });
  for await (const c of chunkFile(file, signal)) {
    chunks.push(c);
    onStatus({ phase: "chunking", done: c.offset + c.size, total: file.size });
  }

  // First occurrence of each distinct chunk: a file can repeat the same
  // content, and it only needs uploading once.
  const firstOf = new Map<string, Chunk>();
  for (const c of chunks) if (!firstOf.has(c.hash)) firstOf.set(c.hash, c);

  // 2. Ask the server which of them it lacks. This is where bandwidth is saved.
  onStatus({ phase: "checking" });
  const missing = await missingChunks([...firstOf.keys()]);

  // 3. Upload just those.
  let chunksSent = 0;
  let bytesSent = 0;
  const send = async (hashes: string[]) => {
    const todo = hashes.map((h) => {
      const c = firstOf.get(h);
      if (!c) throw new Error("The server asked for a chunk that is not in this file");
      return c;
    });
    const totalBytes = bytesSent + todo.reduce((n, c) => n + c.size, 0);
    const totalChunks = chunksSent + todo.length;
    onStatus({ phase: "uploading", chunks: chunksSent, totalChunks, bytes: bytesSent, totalBytes });
    await runPool(
      todo,
      PARALLEL_UPLOADS,
      async (c) => {
        await putChunk(c.hash, file.slice(c.offset, c.offset + c.size), signal);
        chunksSent++;
        bytesSent += c.size;
        onStatus({ phase: "uploading", chunks: chunksSent, totalChunks, bytes: bytesSent, totalBytes });
      },
      signal,
    );
  };
  if (missing.length > 0) await send(missing);

  // 4. Commit: the file is just this ordered list of chunk hashes.
  const manifest: ChunkRef[] = chunks.map((c) => ({ hash: c.hash, size: c.size }));
  const commit = () => {
    const body = {
      size: file.size,
      chunks: manifest,
      chunksSent,
      bytesSent,
      durationMs: Math.round(performance.now() - started),
    };
    return updateFileId
      ? updateCdcFile(updateFileId, body)
      : createCdcFile({ ...body, name: file.name });
  };

  onStatus({ phase: "committing" });
  try {
    await commit();
  } catch (err) {
    // 409: the server lacks some chunks it said it had (for example GC removed
    // them in between). Upload exactly those and try once more.
    if (err instanceof ApiError && err.status === 409 && err.missing?.length) {
      await send(err.missing);
      onStatus({ phase: "committing" });
      await commit();
      return;
    }
    throw err;
  }
}

// PUT one chunk's bytes, retrying on network errors and 5xx. A 4xx is a real
// rejection (for example the bytes no longer match the hash because the file
// was modified while uploading), so retrying would not help.
async function putChunk(hash: string, body: Blob, signal?: AbortSignal): Promise<void> {
  let lastError: unknown;
  for (let attempt = 1; attempt <= MAX_ATTEMPTS; attempt++) {
    try {
      const res = await fetch(chunkUrl(hash), { method: "PUT", body, signal });
      if (res.ok) return;
      const message = await errorFrom(res);
      if (res.status < 500) throw new RejectedError(message);
      lastError = new Error(message);
    } catch (err) {
      if (signal?.aborted || err instanceof RejectedError) throw err;
      lastError = err;
    }
  }
  throw lastError;
}

// A response the server refused on purpose; not worth retrying.
class RejectedError extends Error {}

async function errorFrom(res: Response): Promise<string> {
  try {
    const body = await res.json();
    if (body?.error) return `The server rejected a chunk: ${body.error}`;
  } catch {
    // not JSON
  }
  return `The server rejected a chunk (HTTP ${res.status})`;
}