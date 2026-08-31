import { forwardRef, useEffect, useRef, type ButtonHTMLAttributes, type ReactNode } from "react";

const variants = {
  primary: "border-ink bg-ink text-canvas hover:opacity-85",
  secondary: "border-line-strong bg-transparent text-ink hover:bg-surface-muted",
  ghost: "border-transparent bg-transparent hover:underline",
  danger: "border-danger bg-danger text-canvas hover:opacity-85",
};

const sizes = {
  md: "min-h-8 px-3 text-[13px]",
  sm: "min-h-7 px-2 text-[11px]",
};

export const Button = forwardRef<HTMLButtonElement, ButtonHTMLAttributes<HTMLButtonElement> & { variant?: keyof typeof variants; size?: keyof typeof sizes }>(function Button({ variant = "secondary", size = "md", className = "", type = "button", ...props }, ref) {
  return <button ref={ref} type={type} {...props} className={`rounded-md border font-medium transition-colors active:translate-y-px disabled:cursor-not-allowed disabled:opacity-50 ${sizes[size]} ${variants[variant]} ${className}`} />;
});

export function StatusBadge({ online }: { online: boolean }) {
  return <span className={`inline-flex items-center gap-1.5 text-xs ${online ? "text-success" : "text-offline"}`}><span className={`h-1.5 w-1.5 rounded-full ${online ? "bg-success" : "bg-offline"}`} aria-hidden="true" />{online ? "在线" : "离线"}</span>;
}

export function TabBar({ label, children }: { label: string; children: ReactNode }) {
  return <nav aria-label={label} className="flex flex-wrap gap-x-6 gap-y-1">{children}</nav>;
}

export function Tab({ active, onClick, children }: { active: boolean; onClick: () => void; children: ReactNode }) {
  return <button type="button" onClick={onClick} aria-current={active ? "true" : undefined} className={`min-h-11 border-b-2 px-0.5 text-sm transition-colors ${active ? "border-ink font-medium text-ink" : "border-transparent text-muted hover:text-ink"}`}>{children}</button>;
}

export function EmptyState({ title, detail, action }: { title: string; detail: string; action?: ReactNode }) {
  return <div className="border-y border-line py-16 text-center"><h2 className="font-serif text-xl font-semibold">{title}</h2><p className="mx-auto mt-3 max-w-lg text-sm leading-6 text-muted">{detail}</p>{action && <div className="mt-6">{action}</div>}</div>;
}

export function PageState({ label = "LOADING", message, tone, action }: { label?: string; message: string; tone?: "danger"; action?: ReactNode }) {
  return <div className="mx-auto max-w-xl py-24 text-center"><p aria-live="polite" className={`font-mono text-[11px] uppercase tracking-[0.16em] ${tone === "danger" ? "text-danger" : "text-faint"}`}>{label}</p><p className="mt-3 text-sm text-muted">{message}</p>{action && <div className="mt-5">{action}</div>}</div>;
}

export function ConfirmDialog({ title, detail, confirmLabel = "确认删除", onConfirm, onClose }: { title: string; detail: string; confirmLabel?: string; onConfirm: () => void; onClose: () => void }) {
  const closeRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  useEffect(() => { const previous = document.activeElement as HTMLElement | null; closeRef.current?.focus(); const key = (event: KeyboardEvent) => { if (event.key === "Escape") onClose(); if (event.key === "Tab") { const focusable = panelRef.current?.querySelectorAll<HTMLElement>("button,[href],[tabindex]:not([tabindex='-1'])"); if (!focusable?.length) return; const first = focusable[0], last = focusable[focusable.length - 1]; if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); } else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); } } }; document.addEventListener("keydown", key); return () => { document.removeEventListener("keydown", key); previous?.focus(); }; }, [onClose]);
  return <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" role="dialog" aria-modal="true" aria-labelledby="confirm-title" onMouseDown={(event) => event.target === event.currentTarget && onClose()}>
    <div ref={panelRef} className="w-full max-w-md rounded-[10px] border border-line bg-surface p-5 shadow-[inset_0_1px_0_var(--edge-light),0_24px_48px_rgba(0,0,0,0.35)]"><h2 id="confirm-title" className="font-serif text-lg font-semibold">{title}</h2><p className="mt-2 text-sm leading-6 text-muted">{detail}</p><div className="mt-6 flex justify-end gap-2"><Button ref={closeRef} onClick={onClose}>取消</Button><Button variant="danger" onClick={onConfirm}>{confirmLabel}</Button></div></div>
  </div>;
}

export const fieldClass = "min-h-11 w-full rounded-md border border-line bg-canvas px-3 py-2 text-sm text-ink placeholder:text-faint focus:border-line-strong";
