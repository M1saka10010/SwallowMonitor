import { NavLink, Outlet } from "react-router-dom";
import { useApp } from "./AppContext";
import { useTheme } from "./ThemeProvider";
import { useOverviewStream } from "../live/OverviewStreamProvider";

export function AppShell() {
  const { auth, settings } = useApp();
  const { theme, setTheme } = useTheme();
  const { connection } = useOverviewStream();
  return <div className="min-h-screen bg-canvas text-ink">
    <header className="border-b border-line bg-canvas"><div className="mx-auto flex min-h-14 max-w-[1440px] items-center justify-between gap-4 px-4 sm:px-8">
      <div className="flex min-w-0 items-center gap-6"><NavLink to="/" className="truncate text-base font-semibold tracking-tight">{settings.siteName}</NavLink><NavLink to="/" className="hidden text-sm text-muted hover:text-ink sm:block">概览</NavLink></div>
      <div className="flex items-center gap-2">
        {connection !== "open" && <span className="hidden text-xs text-warning sm:inline">实时连接恢复中</span>}
        <label className="sr-only" htmlFor="theme-select">主题</label><select id="theme-select" value={theme} onChange={(event) => setTheme(event.target.value as "auto" | "light" | "dark")} className="min-h-11 rounded border border-line bg-surface px-2 text-xs"><option value="auto">跟随系统</option><option value="light">浅色</option><option value="dark">深色</option></select>
        {auth.loggedIn ? <><NavLink to="/admin/hosts" className="inline-flex min-h-11 items-center rounded px-2 text-sm text-accent hover:bg-surface-muted">管理</NavLink>{auth.authEnabled && <a href="/logout" className="inline-flex min-h-11 items-center px-2 text-sm text-muted">退出</a>}</> : <a href="/login" className="inline-flex min-h-11 items-center px-2 text-sm text-accent">登录</a>}
      </div>
    </div></header>
    <main className="mx-auto max-w-[1440px] px-4 py-8 sm:px-8"><Outlet /></main>
  </div>;
}
