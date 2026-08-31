import { NavLink, Outlet } from "react-router-dom";
import { useApp } from "./AppContext";
import { useTheme } from "./ThemeProvider";
import { useOverviewStream } from "../live/OverviewStreamProvider";

export function AppShell() {
  const { auth, settings } = useApp();
  const { theme, setTheme } = useTheme();
  const { connection } = useOverviewStream();
  return <div className="min-h-screen bg-canvas text-ink">
    <header className="border-b border-line bg-canvas"><div className="mx-auto flex min-h-12 max-w-[1440px] items-center justify-between gap-4 px-4 sm:px-8">
      <div className="flex min-w-0 items-center gap-6">
        <NavLink to="/" className="truncate font-serif text-xl font-semibold tracking-tight">{settings.siteName}</NavLink>
        <NavLink to="/" end className={({ isActive }) => `hidden border-b-2 pb-0.5 text-sm transition-colors sm:block ${isActive ? "border-ink font-medium text-ink" : "border-transparent text-muted hover:text-ink"}`}>概览</NavLink>
        {auth.loggedIn && <NavLink to="/admin/hosts" className={({ isActive }) => `hidden border-b-2 pb-0.5 text-sm transition-colors sm:block ${isActive ? "border-ink font-medium text-ink" : "border-transparent text-muted hover:text-ink"}`}>管理</NavLink>}
      </div>
      <div className="flex items-center gap-3">
        {connection !== "open" && <span className="hidden items-center gap-1.5 text-xs text-warning sm:flex"><span className="h-1.5 w-1.5 animate-pulse rounded-full bg-warning" aria-hidden="true" />实时连接恢复中</span>}
        <label className="sr-only" htmlFor="theme-select">主题</label>
        <div role="radiogroup" aria-label="主题" className="flex items-center gap-0.5 rounded-md border border-line bg-surface p-0.5 shadow-[inset_0_1px_0_var(--edge-light)]">
          {([["auto", "自动"], ["light", "浅色"], ["dark", "深色"]] as const).map(([value, label]) => <button key={value} type="button" role="radio" aria-checked={theme === value} title={value === "auto" ? "跟随系统" : undefined} onClick={() => setTheme(value)} className={`min-h-8 rounded-[4px] px-2.5 text-xs transition-colors ${theme === value ? "bg-ink font-medium text-canvas" : "text-muted hover:text-ink"}`}>{label}</button>)}
        </div>
        {auth.loggedIn ? <>{auth.authEnabled && <a href="/logout" className="inline-flex min-h-11 items-center text-sm text-muted transition-colors hover:text-ink">退出</a>}</> : <a href="/login" className="inline-flex min-h-11 items-center text-sm font-medium text-ink underline underline-offset-4">登录</a>}
      </div>
    </div></header>
    <main className="mx-auto max-w-[1440px] px-4 py-6 sm:px-8"><Outlet /></main>
  </div>;
}
