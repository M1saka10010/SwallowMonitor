import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { api } from "../../api";
import type { Host, LiveEvent } from "../../api/types";
import { Button, EmptyState, PageState, StatusBadge } from "../../components/ui";
import { useOverviewStream } from "../../live/OverviewStreamProvider";
import { clampPercentage, errorMessage, formatBytes, percentage } from "../../utils/format";
import { useApp } from "../../app/AppContext";

const UNGROUPED = "未分组";

function Metric({ label, value, text }: { label: string; value: number; text: string }) {
  return <div><div className="mb-1 flex justify-between gap-4 text-xs"><span className="text-muted">{label}</span><span className="font-mono tabular-nums">{text}</span></div><div className="h-1 bg-surface-muted"><div className="h-full bg-accent transition-[width]" style={{ width: `${clampPercentage(value)}%` }} /></div></div>;
}

function HostCard({ host }: { host: Host }) {
  const usage = host.latest;
  return <Link to={`/hosts/${encodeURIComponent(host.publicId)}?range=1h`} className="group relative block overflow-hidden rounded-md border border-line bg-surface p-4 transition-colors hover:border-line-strong">
    <span className={`absolute inset-x-0 top-0 h-0.5 ${host.online ? "bg-success" : "bg-offline"}`} />
    <div className="flex items-start justify-between gap-3"><div className="min-w-0"><h2 className="truncate text-sm font-semibold">{host.nickname}</h2><p className="mt-1 truncate text-xs text-muted">{host.hostname || "尚未上报系统信息"} · {host.os || "未知系统"} {host.platformVersion}</p></div><StatusBadge online={host.online} /></div>
    <div className="mt-3 flex min-h-5 flex-wrap gap-1.5">{host.tags.map((tag) => <span key={tag} className="rounded border border-line bg-surface-muted px-1.5 py-0.5 text-[11px] text-muted">{tag}</span>)}</div>
    <div className="mt-4 grid gap-3"><Metric label="CPU" value={usage?.cpuUsage ?? 0} text={`${(usage?.cpuUsage ?? 0).toFixed(1)}%`} /><Metric label="内存" value={percentage(usage?.memoryUsed ?? 0, usage?.memoryTotal ?? 0)} text={`${formatBytes(usage?.memoryUsed ?? 0)} / ${formatBytes(usage?.memoryTotal ?? 0)}`} /><Metric label="磁盘" value={percentage(usage?.diskUsed ?? 0, usage?.diskTotal ?? 0)} text={`${formatBytes(usage?.diskUsed ?? 0)} / ${formatBytes(usage?.diskTotal ?? 0)}`} /></div>
    <div className="mt-4 flex justify-between border-t border-line pt-3 font-mono text-xs tabular-nums text-muted"><span>↓ {formatBytes(usage?.netRecvSpeed ?? 0)}/s</span><span>↑ {formatBytes(usage?.netSendSpeed ?? 0)}/s</span></div>
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
  if (error) return <PageState message={`主机列表加载失败：${error}`} action={<Button onClick={() => { setError(""); void load(); }}>重新加载</Button>} />;
  if (!hosts) return <PageState message="正在加载主机…" />;
  return <section>
    <div className="mb-8 flex flex-col justify-between gap-4 border-b border-line pb-6 sm:flex-row sm:items-end"><div><p className="mb-2 text-xs font-medium uppercase tracking-[0.16em] text-muted">Infrastructure status</p><h1 className="text-2xl font-semibold tracking-tight sm:text-[28px]">主机概览</h1>{settings.siteDescription && <p className="mt-2 max-w-2xl text-sm leading-6 text-muted">{settings.siteDescription}</p>}</div><p className="font-mono text-sm tabular-nums"><span className="text-success">{online} 在线</span><span className="mx-2 text-line-strong">/</span><span className="text-muted">{hosts.length} 总计</span></p></div>
    {!hosts.length ? <EmptyState title="尚未添加主机" detail={auth.loggedIn ? "前往后台添加主机，再使用安装命令连接 Agent。" : "管理员添加主机后，实时状态会显示在这里。"} action={auth.loggedIn && <Link className="text-sm text-accent" to="/admin/hosts">添加主机</Link>} /> : <>
      <nav aria-label="标签筛选" className="mb-6 flex flex-wrap gap-2"><Button variant={!filter ? "primary" : "secondary"} onClick={() => setParams({})}>全部 <span className="ml-1 font-mono">{hosts.length}</span></Button>{tags.map(([tag, count]) => <Button key={tag} variant={filter === tag ? "primary" : "secondary"} onClick={() => setParams({ tag })}>{tag} <span className="ml-1 font-mono">{count}</span></Button>)}</nav>
      {!visible?.length ? <EmptyState title="当前筛选没有主机" detail="选择其他标签或查看全部主机。" action={<Button onClick={() => setParams({})}>查看全部</Button>} /> : <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">{visible.map((host) => <HostCard key={host.publicId} host={host} />)}</div>}
    </>}
  </section>;
}
