import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { api } from "../../api";
import type { Host, LiveEvent } from "../../api/types";
import { Button, EmptyState, PageState, StatusBadge, Tab, TabBar } from "../../components/ui";
import { useOverviewStream } from "../../live/OverviewStreamProvider";
import { clampPercentage, errorMessage, formatBytes, percentage } from "../../utils/format";
import { useApp } from "../../app/AppContext";

const UNGROUPED = "未分组";

function Metric({ label, value, text }: { label: string; value: number; text: string }) {
  return <div className="border-t border-line py-1.5 first:border-t-0 first:pt-0">
    <div className="flex justify-between gap-4 text-xs"><span className="text-muted">{label}</span><span className="font-mono tabular-nums">{text}</span></div>
    <div className="mt-1 h-0.5 bg-surface-muted"><div className="h-full bg-ink" style={{ width: `${clampPercentage(value)}%` }} /></div>
  </div>;
}

function HostCell({ host }: { host: Host }) {
  const usage = host.latest;
  return <Link to={`/hosts/${encodeURIComponent(host.publicId)}?range=1h`} className="relative block overflow-hidden rounded-[10px] border border-line bg-surface p-4 shadow-[0_1px_2px_rgba(0,0,0,0.04),inset_0_1px_0_var(--edge-light)] transition-colors hover:bg-surface-muted">
    <span className={`absolute inset-x-0 top-0 h-0.5 ${host.online ? "bg-success" : "bg-offline"}`} aria-hidden="true" />
    <div className="flex items-start justify-between gap-3"><h2 className="min-w-0 truncate text-sm font-medium">{host.nickname}</h2><StatusBadge online={host.online} /></div>
    <p className="mt-1 truncate font-mono text-xs text-muted">{host.hostname || "尚未上报系统信息"} · {host.platform || host.os || "未知系统"} {host.platformVersion}</p>
    {host.tags.length > 0 && <div className="mt-2 flex min-h-5 flex-wrap gap-1">{host.tags.map((tag) => <span key={tag} className="rounded-full border border-line px-2 py-0.5 text-[11px] text-faint">{tag}</span>)}</div>}
    <div className="mt-3 grid gap-1.5">
      <Metric label="CPU" value={usage?.cpuUsage ?? 0} text={`${(usage?.cpuUsage ?? 0).toFixed(1)}%`} />
      <Metric label="内存" value={percentage(usage?.memoryUsed ?? 0, usage?.memoryTotal ?? 0)} text={`${formatBytes(usage?.memoryUsed ?? 0)} / ${formatBytes(usage?.memoryTotal ?? 0)}`} />
      <Metric label="磁盘" value={percentage(usage?.diskUsed ?? 0, usage?.diskTotal ?? 0)} text={`${formatBytes(usage?.diskUsed ?? 0)} / ${formatBytes(usage?.diskTotal ?? 0)}`} />
    </div>
    <div className="mt-3 flex justify-between border-t border-line pt-2 font-mono text-xs tabular-nums text-muted"><span>↓ {formatBytes(usage?.netRecvSpeed ?? 0)}/s</span><span>↑ {formatBytes(usage?.netSendSpeed ?? 0)}/s</span></div>
  </Link>;
}

export function OverviewPage() {
  const { settings, auth } = useApp();
  const { subscribe, generation } = useOverviewStream();
  const [params, setParams] = useSearchParams();
  const [hosts, setHosts] = useState<Host[]>();
  const [error, setError] = useState("");
  const unknownRefresh = useRef<number | undefined>(undefined);
  const filter = params.get("tag") ?? "";
  const load = useCallback(() => api.hosts().then((nextHosts) => { setHosts(nextHosts); setError(""); }).catch((reason) => setError(errorMessage(reason))), []);
  useEffect(() => { void load(); }, [load, generation]);
  useEffect(() => subscribe((event: LiveEvent) => setHosts((current) => {
    if (!current) return current;
    const index = current.findIndex((host) => host.publicId === event.publicId);
    if (index < 0) {
      if (!unknownRefresh.current) unknownRefresh.current = window.setTimeout(() => { unknownRefresh.current = undefined; void load(); }, 500);
      return current;
    }
    return current.map((host, hostIndex) => hostIndex !== index ? host : event.type === "status" ? { ...host, online: event.online } : { ...host, online: true, latest: event.data });
  })), [load, subscribe]);
  useEffect(() => () => { if (unknownRefresh.current) window.clearTimeout(unknownRefresh.current); }, []);
  const tags = useMemo(() => {
    const counts = new Map<string, number>();
    hosts?.forEach((host) => (host.tags.length ? host.tags : [UNGROUPED]).forEach((tag) => counts.set(tag, (counts.get(tag) ?? 0) + 1)));
    return [...counts].sort(([a], [b]) => a === UNGROUPED ? 1 : b === UNGROUPED ? -1 : a.localeCompare(b));
  }, [hosts]);
  const visible = hosts?.filter((host) => !filter || (host.tags.length ? host.tags : [UNGROUPED]).includes(filter));
  const online = hosts?.filter((host) => host.online).length ?? 0;
  if (error) return <PageState label="ERROR" tone="danger" message={`主机列表加载失败：${error}`} action={<Button onClick={() => { setError(""); void load(); }}>重新加载</Button>} />;
  if (!hosts) return <PageState message="正在加载主机…" />;
  return <section>
    <div className="mb-6 flex flex-col justify-between gap-4 border-b border-line pb-5 sm:flex-row sm:items-end">
      <div>
        <p className="mb-2 font-mono text-[11px] uppercase tracking-[0.16em] text-faint">Infrastructure</p>
        <h1 className="font-serif text-[28px] font-semibold leading-tight tracking-tight">主机概览</h1>
        {settings.siteDescription && <p className="mt-2 max-w-2xl text-sm leading-6 text-muted">{settings.siteDescription}</p>}
      </div>
      <div className="flex flex-col items-start gap-1 sm:items-end">
        <p className="font-mono text-[11px] uppercase tracking-[0.16em] text-faint">在线 / 总计</p>
        <p className="font-mono text-[28px] leading-none tabular-nums"><span className="text-success">{online}</span><span className="text-faint"> / {hosts.length}</span></p>
      </div>
    </div>
    {!hosts.length ? <EmptyState title="尚未添加主机" detail={auth.loggedIn ? "前往后台添加主机，再使用安装命令连接 Agent。" : "管理员添加主机后，实时状态会显示在这里。"} action={auth.loggedIn && <Link className="text-sm text-ink underline underline-offset-4" to="/admin/hosts">添加主机</Link>} /> : <>
      <div className="mb-4"><TabBar label="标签筛选"><Tab active={!filter} onClick={() => setParams({})}>全部 <span className="ml-1.5 font-mono text-xs text-faint">{hosts.length}</span></Tab>{tags.map(([tag, count]) => <Tab key={tag} active={filter === tag} onClick={() => setParams({ tag })}>{tag} <span className="ml-1.5 font-mono text-xs text-faint">{count}</span></Tab>)}</TabBar></div>
      {!visible?.length ? <EmptyState title="当前筛选没有主机" detail="选择其他标签或查看全部主机。" action={<Button onClick={() => setParams({})}>查看全部</Button>} /> : <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">{visible.map((host) => <HostCell key={host.publicId} host={host} />)}</div>}
    </>}
  </section>;
}
