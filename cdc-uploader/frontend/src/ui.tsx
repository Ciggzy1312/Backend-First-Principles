// Small UI pieces shared by both uploader panels.
import type { ReactNode } from "react";

export function Banner({ children }: { children: ReactNode }) {
  return (
    <p className="mt-3 rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700">
      {children}
    </p>
  );
}

const BUTTON_BASE =
  "rounded-lg border px-3 py-1.5 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50";
export const BUTTON_NORMAL = `${BUTTON_BASE} border-slate-300 hover:bg-slate-100`;
const BUTTON_DANGER = `${BUTTON_BASE} border-red-200 text-red-700 hover:bg-red-50`;

export function RowButton(props: {
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

export function Badge({ children }: { children: ReactNode }) {
  return (
    <span className="rounded-full bg-amber-100 px-2.5 py-1 text-xs font-medium text-amber-800">
      {children}
    </span>
  );
}

export function ProgressBar({ percent, label }: { percent: number; label: string }) {
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