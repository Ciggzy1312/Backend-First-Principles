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

// Calls the server and returns parsed JSON. On a non-2xx response it throws an
// Error carrying the server's {"error": "..."} message.
async function call<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_URL}${path}`, init);
  if (!res.ok) {
    let message = `${res.status} ${res.statusText}`;
    try {
      const body = await res.json();
      if (body?.error) message = body.error;
    } catch {
      // the body was not JSON; keep the status text
    }
    throw new Error(message);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

function postJson(body?: unknown): RequestInit {
  return {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  };
}

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