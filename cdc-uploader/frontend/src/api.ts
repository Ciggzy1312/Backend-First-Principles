// Thin wrapper around the Go server's Uploader 1 routes (/api/mp/...).

export const API_URL: string =
  import.meta.env.VITE_API_URL ?? "http://localhost:8080";

export type LastUpload = {
  bytesSent: number;
  durationMs: number;
  // Present only after an update: how many parts of the new version had the
  // same content as the old version, yet were sent again.
  update?: { partsTotal: number; partsIdentical: number };
};

export type MpFile = {
  id: string;
  name: string;
  size: number;
  version: number;
  status: "uploading" | "ready"; // "uploading" = the first upload never finished
  updating: boolean; // a ready file with an update started but not finished
  lastUpload?: LastUpload;
  updatedAt: string;
};

export type PartUrl = { n: number; url: string };
export type PartEtag = { n: number; etag: string };

// An error response from the Go server.
export class ApiError extends Error {
  status: number;
  // Set on a 409 from the Uploader 2 commit/update: chunks the server lacks.
  missing?: string[];

  constructor(message: string, status: number, missing?: string[]) {
    super(message);
    this.status = status;
    this.missing = missing;
  }
}

// Calls the server and returns parsed JSON. On a non-2xx response it throws an
// ApiError carrying the server's {"error": "..."} message.
async function call<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_URL}${path}`, init);
  if (!res.ok) {
    let message = `${res.status} ${res.statusText}`;
    let missing: string[] | undefined;
    try {
      const body = await res.json();
      if (body?.error) message = body.error;
      if (Array.isArray(body?.missing)) missing = body.missing;
    } catch {
      // the body was not JSON; keep the status text
    }
    throw new ApiError(message, res.status, missing);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

function jsonBody(method: string, body?: unknown): RequestInit {
  return {
    method,
    headers: { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  };
}

const postJson = (body?: unknown) => jsonBody("POST", body);

export async function listFiles(): Promise<MpFile[]> {
  const data = await call<{ files: MpFile[] }>("/api/mp/files");
  return data.files;
}

export type UploadStart = { fileId: string; parts: PartUrl[] };
type PartsInfo = { size: number; partSize: number; partHashes: string[] };

// Step 1 of an upload: tell the server about the file, get one upload URL per part.
export function initiateUpload(
  req: PartsInfo & { name: string },
): Promise<UploadStart> {
  return call("/api/mp/files", postJson(req));
}

// Step 1 of an update: same idea for a file that already exists. The server
// keeps the file's current name, and keeps serving the old version until the
// update completes.
export function updateUpload(
  fileId: string,
  req: PartsInfo,
): Promise<UploadStart> {
  return call(`/api/mp/files/${fileId}/update`, postJson(req));
}

// Step 3 of an upload: hand the server the ETag receipts so S3 can assemble the file.
export function completeUpload(
  fileId: string,
  parts: PartEtag[],
  durationMs: number,
): Promise<{ file: MpFile }> {
  return call(`/api/mp/files/${fileId}/complete`, postJson({ parts, durationMs }));
}

// Cancel an upload or update that did not finish, so S3 does not keep the
// stored parts. For a file that never finished uploading this removes it; for
// an update it drops the update and the file keeps its current version.
export function abortUpload(fileId: string): Promise<void> {
  return call(`/api/mp/files/${fileId}/abort`, postJson());
}

export function deleteFile(fileId: string): Promise<void> {
  return call(`/api/mp/files/${fileId}`, { method: "DELETE" });
}

// Download is a plain link, not a fetch(): the server answers with a redirect
// to S3 and the browser saves the file itself, without loading it into memory.
export function downloadUrl(fileId: string): string {
  return `${API_URL}/api/mp/files/${fileId}/download`;
}

// ---------------------------------------------------------------- Uploader 2 (/api/cdc/...)

export type ChunkRef = { hash: string; size: number };

export type CdcFile = {
  id: string;
  name: string;
  size: number;
  version: number;
  chunkCount: number;
  lastUpload?: {
    chunksTotal: number;
    chunksSent: number; // chunks the server did not have, so they were uploaded
    bytesSent: number;
    durationMs: number;
  };
  updatedAt: string;
};

// Body of a commit (new file) or an update. For an update, name may be left out.
export type CommitRequest = {
  name?: string;
  size: number;
  chunks: ChunkRef[]; // the whole file, in order
  chunksSent: number;
  bytesSent: number;
  durationMs: number;
};

export async function listCdcFiles(): Promise<CdcFile[]> {
  const data = await call<{ files: CdcFile[] }>("/api/cdc/files");
  return data.files;
}

// Ask which of these chunk hashes the server does NOT have yet.
export async function missingChunks(hashes: string[]): Promise<string[]> {
  const data = await call<{ missing: string[] }>(
    "/api/cdc/chunks/missing",
    postJson({ hashes }),
  );
  return data.missing;
}

// The upload of one chunk goes through fetch directly (see cdc.ts), because it
// sends raw bytes and needs its own retry rules. Its URL:
export function chunkUrl(hash: string): string {
  return `${API_URL}/api/cdc/chunks/${hash}`;
}

// Commit a new file. Fails with a 409 ApiError (with .missing) if any chunk
// in the list is not on the server.
export function createCdcFile(
  req: CommitRequest & { name: string },
): Promise<{ file: CdcFile }> {
  return call("/api/cdc/files", postJson(req));
}

// Replace an existing file's content (same 409 rule).
export function updateCdcFile(
  fileId: string,
  req: CommitRequest,
): Promise<{ file: CdcFile }> {
  return call(`/api/cdc/files/${fileId}`, jsonBody("PUT", req));
}

export function deleteCdcFile(fileId: string): Promise<void> {
  return call(`/api/cdc/files/${fileId}`, { method: "DELETE" });
}

// A plain link, like Uploader 1: the browser saves the streamed response itself.
export function cdcDownloadUrl(fileId: string): string {
  return `${API_URL}/api/cdc/files/${fileId}/download`;
}

export type GcResult = {
  deleted: number;
  bytesFreed: number;
  skipped: number;
  failed: number;
  more: boolean;
};

// Delete every chunk no file uses, right now (grace period 0). For
// experiments only: do not run it while an upload is in progress.
export function runGc(): Promise<GcResult> {
  return call("/api/cdc/gc?grace=0s", postJson());
}