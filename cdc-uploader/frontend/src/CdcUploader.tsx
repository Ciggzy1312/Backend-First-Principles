import { useCallback, useEffect, useRef, useState } from "react";
import type { ChangeEvent } from "react";
import {
  cdcDownloadUrl,
  deleteCdcFile,
  listCdcFiles,
  runGc,
  type CdcFile,
} from "./api";
import { uploadCdc, type CdcStatus } from "./cdc";
import { errorMessage, formatBytes, formatDuration } from "./lib/format";
import { Banner, BUTTON_NORMAL, ProgressBar, RowButton } from "./ui";

export default function CdcUploader() {
  const [files, setFiles] = useState<CdcFile[]>([]);
  const [listError, setListError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [gcResult, setGcResult] = useState<string | null>(null);
  const [status, setStatus] = useState<CdcStatus>({ phase: "idle" });
  const inputRef = useRef<HTMLInputElement>(null);
  // Which file the next picked file should replace; null means "new upload".
  const updateIdRef = useRef<string | null>(null);
  // Lets the Cancel button stop the upload in progress.
  const abortRef = useRef<AbortController | null>(null);

  const busy = status.phase !== "idle" && status.phase !== "error";

  const refresh = useCallback(async () => {
    try {
      setFiles(await listCdcFiles());
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
    setGcResult(null);
    const controller = new AbortController();
    abortRef.current = controller;
    try {
      await uploadCdc(file, setStatus, {
        signal: controller.signal,
        updateFileId: updateId ?? undefined,
      });
      setStatus({ phase: "idle" });
    } catch (err) {
      if (controller.signal.aborted) {
        setStatus({ phase: "idle" }); // the user cancelled; not an error
      } else {
        setStatus({ phase: "error", message: errorMessage(err) });
      }
    }
    abortRef.current = null;
    await refresh();
  }

  // Run a quick action, show its error if any, then refresh the list.
  async function act(action: () => Promise<unknown>) {
    setActionError(null);
    try {
      await action();
    } catch (err) {
      setActionError(errorMessage(err));
    }
    await refresh();
  }

  function onDelete(file: CdcFile) {
    if (!window.confirm(`Delete "${file.name}"? This cannot be undone.`)) return;
    setGcResult(null);
    void act(() => deleteCdcFile(file.id));
  }

  function onCleanUp() {
    void act(async () => {
      const r = await runGc();
      setGcResult(
        r.deleted === 0
          ? "No unused chunks to remove."
          : `Removed ${r.deleted} unused chunk${r.deleted === 1 ? "" : "s"}, freeing ${formatBytes(r.bytesFreed)}.` +
              (r.more ? " More remain; run it again." : ""),
      );
    });
  }

  return (
    <section>
      <h2 className="text-2xl font-semibold">
        Uploader 2: Content-defined chunking
      </h2>
      <p className="mt-2 text-slate-600">
        Cuts the file at content-defined boundaries into chunks of about 64
        KiB, asks the server which chunks it already has, and uploads only the
        missing ones. Updating a file sends just the chunks that changed, even
        if bytes were inserted near the start.
      </p>

      <div className="mt-6">
        <input ref={inputRef} type="file" className="hidden" onChange={onPick} />
        <div className="flex items-center gap-3">
          <button
            type="button"
            onClick={() => chooseFile(null)}
            disabled={busy}
            className="rounded-lg bg-indigo-600 px-4 py-2 font-medium text-white shadow-sm hover:bg-indigo-700 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {busy ? "Uploading…" : "Upload file"}
          </button>
          {busy && (
            <RowButton onClick={() => abortRef.current?.abort()} disabled={false}>
              Cancel
            </RowButton>
          )}
        </div>

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
            />
          ))}
        </ul>
      )}

      <div className="mt-6 flex flex-wrap items-center gap-3 text-sm text-slate-600">
        <RowButton onClick={onCleanUp} disabled={busy}>
          Clean up unused chunks
        </RowButton>
        <span>
          Deleting or updating a file leaves chunks nobody uses. This removes
          them from storage now.
        </span>
      </div>
      {gcResult && <p className="mt-2 text-sm text-slate-700">{gcResult}</p>}
    </section>
  );
}

function Progress({ status }: { status: CdcStatus }) {
  if (status.phase === "idle") return null;

  if (status.phase === "error") {
    return <Banner>Upload failed: {status.message}</Banner>;
  }

  let label: string;
  let percent: number;
  if (status.phase === "chunking") {
    label = `Cutting and hashing chunks (${formatBytes(status.done)} of ${formatBytes(status.total)})…`;
    percent = (status.done / status.total) * 100;
  } else if (status.phase === "checking") {
    label = "Asking the server which chunks it already has…";
    percent = 100;
  } else if (status.phase === "uploading") {
    label = `Uploading chunk ${Math.min(status.chunks + 1, status.totalChunks)} of ${status.totalChunks} (${formatBytes(status.bytes)} of ${formatBytes(status.totalBytes)})…`;
    percent = (status.bytes / status.totalBytes) * 100;
  } else {
    label = "Finishing…";
    percent = 100;
  }

  return <ProgressBar percent={percent} label={label} />;
}

function FileRow(props: {
  file: CdcFile;
  busy: boolean;
  onUpdate: () => void;
  onDelete: () => void;
}) {
  const { file, busy } = props;
  const last = file.lastUpload;
  const savedSomething = last !== undefined && last.chunksSent < last.chunksTotal;

  return (
    <li className="flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:gap-4">
      <div className="min-w-0">
        <p className="truncate font-medium">{file.name}</p>
        <p className="text-sm text-slate-500">
          {formatBytes(file.size)} · v{file.version} · {file.chunkCount}{" "}
          {file.chunkCount === 1 ? "chunk" : "chunks"}
        </p>
        {last && (
          <p className={"text-sm " + (savedSomething ? "text-emerald-700" : "text-slate-500")}>
            Sent {last.chunksSent} of {last.chunksTotal} chunks (
            {formatBytes(last.bytesSent)}) in {formatDuration(last.durationMs)}
          </p>
        )}
      </div>

      <div className="flex shrink-0 flex-wrap items-center gap-2">
        <a href={cdcDownloadUrl(file.id)} className={BUTTON_NORMAL}>
          Download
        </a>
        <RowButton onClick={props.onUpdate} disabled={busy}>
          Update
        </RowButton>
        <RowButton onClick={props.onDelete} disabled={busy} danger>
          Delete
        </RowButton>
      </div>
    </li>
  );
}