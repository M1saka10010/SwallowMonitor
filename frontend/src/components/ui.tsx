import { forwardRef, useEffect, useRef, type ButtonHTMLAttributes, type ReactNode } from "react";

const variants = {
  primary: "border-accent bg-accent text-white hover:opacity-90",
  secondary: "border-line bg-surface text-ink hover:border-line-strong",
  ghost: "border-transparent bg-transparent text-accent hover:bg-surface-muted",
  danger: "border-danger bg-danger text-white hover:opacity-90",
};

export const Button = forwardRef<HTMLButtonElement, ButtonHTMLAttributes<HTMLButtonElement> & { variant?: keyof typeof variants }>(function Button({ variant = "secondary", className = "", type = "button", ...props }, ref) {
  return <button ref={ref} type={type} {...props} className={`min-h-11 rounded border px-3 py-2 text-sm font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50 ${variants[variant]} ${className}`} />;
});

export function StatusBadge({ online }: { online: boolean }) {
  return <span className="inline-flex items-center gap-2 text-xs text-muted"><span className={`h-1.5 w-1.5 rounded-full ${online ? "bg-success" : "bg-offline"}`} />{online ? "在线" : "离线"}</span>;
}

export function EmptyState({ title, detail, action }: { title: string; detail: string; action?: ReactNode }) {
  return <div className="border-y border-line py-16 text-center"><h2 className="text-base font-semibold">{title}</h2><p className="mx-auto mt-2 max-w-lg text-sm text-muted">{detail}</p>{action && <div className="mt-5">{action}</div>}</div>;
}

export function PageState({ message, action }: { message: string; action?: ReactNode }) {
  return <div className="mx-auto max-w-xl py-24 text-center"><p className="text-sm text-muted">{message}</p>{action && <div className="mt-4">{action}</div>}</div>;
}

export function ConfirmDialog({ title, detail, confirmLabel = "确认删除", onConfirm, onClose }: { title: string; detail: string; confirmLabel?: string; onConfirm: () => void; onClose: () => void }) {
  const closeRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  useEffect(() => { const previous = document.activeElement as HTMLElement | null; closeRef.current?.focus(); const key = (event: KeyboardEvent) => { if (event.key === "Escape") onClose(); if (event.key === "Tab") { const focusable = panelRef.current?.querySelectorAll<HTMLElement>("button,[href],[tabindex]:not([tabindex='-1'])"); if (!focusable?.length) return; const first = focusable[0], last = focusable[focusable.length - 1]; if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); } else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); } } }; document.addEventListener("keydown", key); return () => { document.removeEventListener("keydown", key); previous?.focus(); }; }, [onClose]);
  return <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" role="dialog" aria-modal="true" aria-labelledby="confirm-title" onMouseDown={(event) => event.target === event.currentTarget && onClose()}>
    <div ref={panelRef} className="w-full max-w-md rounded-md border border-line bg-surface p-5 shadow-2xl"><h2 id="confirm-title" className="text-lg font-semibold">{title}</h2><p className="mt-2 text-sm leading-6 text-muted">{detail}</p><div className="mt-6 flex justify-end gap-2"><Button ref={closeRef} onClick={onClose}>取消</Button><Button variant="danger" onClick={onConfirm}>{confirmLabel}</Button></div></div>
  </div>;
}

export const fieldClass = "min-h-11 w-full rounded border border-line bg-canvas px-3 py-2 text-sm text-ink placeholder:text-muted focus:border-accent";
