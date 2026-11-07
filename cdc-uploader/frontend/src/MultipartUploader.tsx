import { useCallback, useEffect, useRef, useState } from "react";
import type { ChangeEvent, ReactNode } from "react";
import {
  abortUpload,
  deleteFile,
  downloadUrl,
  listFiles,
  type MpFile,
} from "./api";
import { errorMessage, formatBytes, formatDuration } from "./lib/format";
import { uploadFile, type UploadStatus } from "./multipart";

export default function MultipartUploader() {
  const [files, setFiles] = useState<MpFile[]>([]);
  const [listError, setListError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [status, setStatus] = useState<UploadStatus>({ phase: "idle" });
  const inputRef = useRef<HTMLInputElement>(null);
  // Which file the next picked file should replace; null means "new upload".
  const updateIdRef = useRef<string | null>(null);

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

  // One hidden file input serves both "Upload file" and every row's "Update".
  function chooseFile(updateId: string | null) {
    updateIdRef.current = updateId;
    inputRef.current?.click();
  }

  async function onPick(e: ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    const updateId = updateIdRef.current;
    e.target.value = ""; // so picking the same file twice still fires onChange
    if (!file) return;

    setActionError(null);
    try {
      await uploadFile(file, setStatus, updateId ?? undefined);
      setStatus({ phase: "idle" });
    } catch (err) {
      setStatus({ phase: "error", message: errorMessage(err) });
    }
    await refresh(); // show the result (or the state left after a failure)
  }

  // Run a quick action (delete, abort), show its error if any, then refresh.
  async function act(action: () => Promise<unknown>) {
    setActionError(null);
    try {
      await action();
    } catch (err) {
      setActionError(errorMessage(err));
    }
    await refresh();
  }

  function onDelete(file: MpFile) {
    if (!window.confirm(`Delete "${file.name}"? This cannot be undone.`)) return;
    void act(() => deleteFile(file.id));
  }

  return (
    <section>
      <h2 className="text-2xl font-semibold">Uploader 1: Multipart upload</h2>
      <p className="mt-2 text-slate-600">
        Splits the file into 8 MiB parts and uploads them straight to S3 as a
        multipart upload. Every upload sends the whole file. Updating a file
        works the same way: even if only a few bytes changed, all parts are
        sent again. After an update, the list shows how many of those parts
        were identical to the old version.
      </p>

      <div className="mt-6">
        <input ref={inputRef} type="file" className="hidden" onChange={onPick} />
        <button
          type="button"
          onClick={() => chooseFile(null)}
          disabled={busy}
          className="rounded-lg bg-indigo-600 px-4 py-2 font-medium text-white shadow-sm hover:bg-indigo-700 disabled:cursor-not-allowed disabled:opacity-50"
        >
          {busy ? "Uploading…" : "Upload file"}
        </button>

        <Progress status={status} />
        {actionError && <Banner>{actionError}</Banner>}
      </div>

      <h3 className="mt-8 text-lg font-semibold">Files</h3>
      {listError && <Banner>Could not load files: {listError}</Banner>}
      {files.length === 0 && !listError ? (
        <p className="mt-2 text-slate-500">No files yet.</p>
      ) : (
        <ul className="mt-2 divide-y divide-slate-200 rounded-lg border border-slate-200 bg-white">
          {files.map((f) => (
            <FileRow
              key={f.id}
              file={f}
              busy={busy}
              onUpdate={() => chooseFile(f.id)}
              onDelete={() => onDelete(f)}
              onAbort={() => void act(() => abortUpload(f.id))}
            />
          ))}
        </ul>
      )}
    </section>
  );
}

function Banner({ children }: { children: ReactNode }) {
  return (
    <p className="mt-3 rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700">
      {children}
    </p>
  );
}

function Progress({ status }: { status: UploadStatus }) {
  if (status.phase === "idle") return null;

  if (status.phase === "error") {
    return <Banner>Upload failed: {status.message}</Banner>;
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

const BUTTON_BASE =
  "rounded-lg border px-3 py-1.5 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50";
const BUTTON_NORMAL = `${BUTTON_BASE} border-slate-300 hover:bg-slate-100`;
const BUTTON_DANGER = `${BUTTON_BASE} border-red-200 text-red-700 hover:bg-red-50`;

function RowButton(props: {
  onClick: () => void;
  disabled: boolean;
  danger?: boolean;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={props.onClick}
      disabled={props.disabled}
      className={props.danger ? BUTTON_DANGER : BUTTON_NORMAL}
    >
      {props.children}
    </button>
  );
}

function Badge({ children }: { children: ReactNode }) {
  return (
    <span className="rounded-full bg-amber-100 px-2.5 py-1 text-xs font-medium text-amber-800">
      {children}
    </span>
  );
}

// A file is in one of three shapes:
//   ready        -> Download, Update, Delete
//   updating     -> a ready file whose update never finished: Download the old
//                   version, or cancel the update (which also unblocks new ones)
//   incomplete   -> the first upload never finished: only Abort makes sense
function FileRow(props: {
  file: MpFile;
  busy: boolean;
  onUpdate: () => void;
  onDelete: () => void;
  onAbort: () => void;
}) {
  const { file, busy } = props;
  const ready = file.status === "ready";
  const stats = file.lastUpload?.update;

  return (
    <li className="flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:gap-4">
      <div className="min-w-0">
        <p className="truncate font-medium">{file.name}</p>
        <p className="text-sm text-slate-500">
          {formatBytes(file.size)} · v{file.version}
          {file.lastUpload &&
            ` · sent ${formatBytes(file.lastUpload.bytesSent)} in ${formatDuration(file.lastUpload.durationMs)}`}
        </p>
        {stats && (
          <p className="text-sm text-amber-700">
            {stats.partsIdentical} of {stats.partsTotal} parts were identical
            but were re-sent anyway
          </p>
        )}
      </div>

      <div className="flex shrink-0 flex-wrap items-center gap-2">
        {ready && (
          <a href={downloadUrl(file.id)} className={BUTTON_NORMAL}>
            Download
          </a>
        )}

        {ready && !file.updating && (
          <RowButton onClick={props.onUpdate} disabled={busy}>
            Update
          </RowButton>
        )}
        {ready && !file.updating && (
          <RowButton onClick={props.onDelete} disabled={busy} danger>
            Delete
          </RowButton>
        )}

        {file.updating && <Badge>Update unfinished</Badge>}
        {file.updating && (
          <RowButton onClick={props.onAbort} disabled={busy}>
            Cancel update
          </RowButton>
        )}

        {!ready && <Badge>Incomplete</Badge>}
        {!ready && (
          <RowButton onClick={props.onAbort} disabled={busy}>
            Abort
          </RowButton>
        )}
      </div>
    </li>
  );
}