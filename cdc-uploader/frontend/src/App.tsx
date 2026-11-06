import { useState } from "react";
import CdcUploader from "./CdcUploader";
import MultipartUploader from "./MultipartUploader";

type Tab = "mp" | "cdc";

const TABS: { id: Tab; label: string }[] = [
  { id: "mp", label: "Uploader 1" },
  { id: "cdc", label: "Uploader 2" },
];

export default function App() {
  const [tab, setTab] = useState<Tab>("mp");

  return (
    <main className="mx-auto max-w-3xl px-4 py-10">
      <div className="inline-flex rounded-lg bg-slate-200 p-1" role="group" aria-label="Choose uploader">
        {TABS.map((t) => (
          <button
            key={t.id}
            type="button"
            aria-pressed={tab === t.id}
            onClick={() => setTab(t.id)}
            className={
              "rounded-md px-4 py-1.5 text-sm font-medium transition-colors " +
              (tab === t.id
                ? "bg-white text-slate-900 shadow-sm"
                : "text-slate-600 hover:text-slate-900")
            }
          >
            {t.label}
          </button>
        ))}
      </div>

      {/* Both stay mounted and the inactive one is hidden, so switching tabs
          in the middle of an upload does not lose its progress. */}
      <div className="mt-8">
        <div className={tab === "mp" ? "" : "hidden"}>
          <MultipartUploader />
        </div>
        <div className={tab === "cdc" ? "" : "hidden"}>
          <CdcUploader />
        </div>
      </div>
    </main>
  );
}