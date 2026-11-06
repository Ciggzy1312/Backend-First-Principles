import { useCallback, useEffect, useRef, useState } from "react";
import type { ChangeEvent } from "react";
import { downloadUrl, listFiles, type MpFile } from "./api";
import { errorMessage, formatBytes, formatDuration } from "./lib/format";
import { uploadFile, type UploadStatus } from "./multipart";

export default function MultipartUploader() {
  const [files, setFiles] = useState<MpFile[]>([]);
  const [listError, setListError] = useState<string | null>(null);
  const [status, setStatus] = useState<UploadStatus>({ phase: "idle" });
  const inputRef = useRef<HTMLInputElement>(null);

  const busy = status.phase !== "idle" && status.phase !== "error";

  const refresh = useCallback(async () => {
    try {
      setFiles(await listFiles());
      setListError(null);
    } catch (err) {
      setListError(errorMessage(err));
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  async function onPick(e: ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    e.target.value = ""; // so picking the same file twice still fires onChange
    if (!file) return;

    try {
      await uploadFile(file, setStatus);
      setStatus({ phase: "idle" });
    } catch (err) {
      setStatus({ phase: "error", message: errorMessage(err) });
    }
    await refresh(); // show the new file (or the leftover state after a failure)
  }

  return (
    <section>
      <h2 className="text-2xl font-semibold">Uploader 1: Multipart upload</h2>
      <p className="mt-2 text-slate-600">
        Splits the file into 8 MiB parts and uploads them straight to S3 as a
        multipart upload. Every upload sends the whole file: even if only a
        few bytes changed, all parts are sent again.
      </p>

      <div className="mt-6">
        <input ref={inputRef} type="file" className="hidden" onChange={onPick} />
        <button
          type="button"
          onClick={() => inputRef.current?.click()}
          disabled={busy}
          className="rounded-lg bg-indigo-600 px-4 py-2 font-medium text-white shadow-sm hover:bg-indigo-700 disabled:cursor-not-allowed disabled:opacity-50"
        >
          {busy ? "Uploading…" : "Upload file"}
        </button>

        <Progress status={status} />
      </div>

      <h3 className="mt-8 text-lg font-semibold">Files</h3>
      {listError && (
        <p className="mt-2 rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700">
          Could not load files: {listError}
        </p>
      )}
      {files.length === 0 && !listError ? (
        <p className="mt-2 text-slate-500">No files yet.</p>
      ) : (
        <ul className="mt-2 divide-y divide-slate-200 rounded-lg border border-slate-200 bg-white">
          {files.map((f) => (
            <FileRow key={f.id} file={f} />
          ))}
        </ul>
      )}
    </section>
  );
}

function Progress({ status }: { status: UploadStatus }) {
  if (status.phase === "idle") return null;

  if (status.phase === "error") {
    return (
      <p className="mt-3 rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700">
        Upload failed: {status.message}
      </p>
    );
  }

  let label: string;
  let percent: number;
  if (status.phase === "hashing") {
    label = `Hashing parts (${status.done}/${status.total})…`;
    percent = (status.done / status.total) * 100;
  } else if (status.phase === "uploading") {
    label = `Uploading ${formatBytes(status.bytes)} of ${formatBytes(status.total)}…`;
    percent = (status.bytes / status.total) * 100;
  } else {
    label = "Finishing…";
    percent = 100;
  }

  return (
    <div className="mt-3 max-w-md">
      <div className="h-2 overflow-hidden rounded-full bg-slate-200">
        <div
          className="h-full bg-indigo-600 transition-all"
          style={{ width: `${percent}%` }}
        />
      </div>
      <p className="mt-1 text-sm text-slate-600">{label}</p>
    </div>
  );
}

function FileRow({ file }: { file: MpFile }) {
  const ready = file.status === "ready";

  return (
    <li className="flex items-center justify-between gap-4 px-4 py-3">
      <div className="min-w-0">
        <p className="truncate font-medium">{file.name}</p>
        <p className="text-sm text-slate-500">
          {formatBytes(file.size)} · v{file.version}
          {file.lastUpload &&
            ` · sent ${formatBytes(file.lastUpload.bytesSent)} in ${formatDuration(file.lastUpload.durationMs)}`}
        </p>
      </div>

      {ready ? (
        <a
          href={downloadUrl(file.id)}
          className="shrink-0 rounded-lg border border-slate-300 px-3 py-1.5 text-sm font-medium hover:bg-slate-100"
        >
          Download
        </a>
      ) : (
        <span className="shrink-0 rounded-full bg-amber-100 px-2.5 py-1 text-xs font-medium text-amber-800">
          Incomplete
        </span>
      )}
    </li>
  );
}