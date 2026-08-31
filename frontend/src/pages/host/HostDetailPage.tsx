import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Line } from "react-chartjs-2";
import { CategoryScale, Chart as ChartJS, Filler, Legend, LinearScale, LineElement, PointElement, Tooltip, type ChartData, type ChartOptions } from "chart.js";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { api } from "../../api";
import type { Host, LiveEvent, RangeKey, Usage } from "../../api/types";
import { EmptyState, PageState, StatusBadge, Tab, TabBar } from "../../components/ui";
import { useOverviewStream } from "../../live/OverviewStreamProvider";
import { errorMessage, formatBytes } from "../../utils/format";
import { buildTimeline, isRangeKey, RANGES } from "./chartData";
import { useTheme } from "../../app/ThemeProvider";

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Filler, Tooltip, Legend);
type Point = { x: number; y: number | null };

const css = (name: string) => getComputedStyle(document.documentElement).getPropertyValue(name).trim();
const line = (label: string, color: string, points: Point[], borderWidth = 1.5, borderDash?: number[]) => ({ label, data: points, borderColor: color, backgroundColor: color, borderWidth, borderDash, pointRadius: 0, tension: 0, spanGaps: false, parsing: false as const });

function chartModel(timeline: ReturnType<typeof buildTimeline>, metric: "cpu" | "memory" | "network" | "load"): ChartData<"line", Point[]> {
  const normal = (value: (usage: Usage) => number) => timeline.map((point) => ({ x: point.timestamp * 1000, y: point.usage ? value(point.usage) : null }));
  const offline = timeline.map((point) => ({ x: point.timestamp * 1000, y: point.offline ? 0 : null }));
  const blue = css("--chart-blue") || "#3478d4";
  const green = css("--chart-green") || "#398760";
  const orange = css("--chart-orange") || "#b56a28";
  const red = css("--chart-red") || "#c64b45";
  const datasets = metric === "cpu" ? [line("CPU %", blue, normal((u) => u.cpuUsage))]
    : metric === "memory" ? [line("内存", green, normal((u) => u.memoryUsed)), line("Swap", orange, normal((u) => u.swapUsed), 1)]
    : metric === "network" ? [line("下行/s", blue, normal((u) => u.netRecvSpeed)), line("上行/s", red, normal((u) => u.netSendSpeed), 1)]
    : [line("load1", blue, normal((u) => u.load1)), line("load5", green, normal((u) => u.load5), 1), line("load15", orange, normal((u) => u.load15), 0.75)];
  return { datasets: [...datasets, line("离线", css("--chart-offline") || "#8f8f89", offline, 1, [4, 4])] };
}

function makeChartOptions(metric: "cpu" | "memory" | "network" | "load", from: number, to: number): ChartOptions<"line"> {
  const bytes = metric === "memory" || metric === "network";
  return { responsive: true, maintainAspectRatio: false, animation: false, interaction: { intersect: false, mode: "index" }, scales: { x: { type: "linear", min: from * 1000, max: to * 1000, ticks: { color: css("--text-muted"), maxTicksLimit: 6, callback: (value) => new Date(Number(value)).toLocaleString([], { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" }) }, grid: { color: css("--border") } }, y: { beginAtZero: true, suggestedMax: metric === "cpu" ? 100 : undefined, ticks: { color: css("--text-muted"), callback: (value) => bytes ? formatBytes(Number(value)) : metric === "cpu" ? `${value}%` : String(value) }, grid: { color: css("--border") } } }, plugins: { legend: { labels: { color: css("--text"), boxWidth: 16, filter: (item, data) => item.datasetIndex === undefined || (data.datasets[item.datasetIndex].data as Point[]).some((point) => point.y !== null) } } } };
}

function MetricChart({ title, metric, timeline, from, to, onOpen }: { title: string; metric: "cpu" | "memory" | "network" | "load"; timeline: ReturnType<typeof buildTimeline>; from: number; to: number; onOpen: () => void }) {
  const { theme, revision } = useTheme();
  const data = useMemo(() => chartModel(timeline, metric), [timeline, metric, theme, revision]);
  const options = useMemo(() => makeChartOptions(metric, from, to), [metric, from, to, theme, revision]);
  return <button type="button" onClick={onOpen} className="overflow-hidden rounded-[10px] border border-line bg-surface p-3 text-left shadow-[0_1px_2px_rgba(0,0,0,0.04),inset_0_1px_0_var(--edge-light)] transition-colors hover:bg-surface-muted" aria-label={`放大${title}图表`}><span className="mb-2 block text-sm font-medium">{title}</span><span className="block h-[236px]"><Line data={data} options={options} /></span></button>;
}

function ChartDialog({ title, children, onClose }: { title: string; children: React.ReactNode; onClose: () => void }) {
  const close = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  useEffect(() => { const previous = document.activeElement as HTMLElement | null; close.current?.focus(); const listener = (event: KeyboardEvent) => { if (event.key === "Escape") onClose(); if (event.key === "Tab") { const focusable = panel.current?.querySelectorAll<HTMLElement>("button,[href],[tabindex]:not([tabindex='-1'])"); if (!focusable?.length) return; const first = focusable[0], last = focusable[focusable.length - 1]; if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); } else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); } } }; document.addEventListener("keydown", listener); document.body.style.overflow = "hidden"; return () => { document.removeEventListener("keydown", listener); document.body.style.overflow = ""; previous?.focus(); }; }, [onClose]);
  return <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4" role="dialog" aria-modal="true" aria-labelledby="chart-title" onMouseDown={(event) => event.target === event.currentTarget && onClose()}><div ref={panel} className="h-[82vh] w-full max-w-6xl rounded-[10px] border border-line bg-surface p-4 shadow-[inset_0_1px_0_var(--edge-light),0_32px_64px_rgba(0,0,0,0.4)]"><div className="mb-3 flex items-center justify-between"><h2 id="chart-title" className="font-serif text-lg font-semibold">{title}</h2><button type="button" ref={close} onClick={onClose} className="min-h-9 text-sm text-muted transition-colors hover:text-ink hover:underline">关闭</button></div><div className="h-[calc(82vh-68px)]">{children}</div></div></div>;
}

export function HostDetailPage() {
  const { publicId = "" } = useParams(); const [params, setParams] = useSearchParams();
  const key: RangeKey = isRangeKey(params.get("range")) ? params.get("range") as RangeKey : "1h";
  const { subscribe, generation } = useOverviewStream();
  const { theme, revision } = useTheme();
  const [host, setHost] = useState<Host>(); const [points, setPoints] = useState<Usage[]>([]); const [windowRange, setWindowRange] = useState({ from: 0, to: 0 }); const [error, setError] = useState(""); const [zoom, setZoom] = useState<null | { title: string; metric: "cpu" | "memory" | "network" | "load" }>(null);
  const load = useCallback((signal: AbortSignal) => { const to = Math.floor(Date.now() / 1000); const from = to - RANGES[key].seconds; setWindowRange({ from, to }); setError(""); return Promise.all([api.host(publicId, signal), api.usage(publicId, from, to, signal)]).then(([nextHost, nextPoints]) => { setHost(nextHost); setPoints(nextPoints); }).catch((reason: unknown) => { if (!(reason instanceof DOMException && reason.name === "AbortError")) setError(errorMessage(reason)); }); }, [key, publicId]);
  useEffect(() => { const controller = new AbortController(); void load(controller.signal); return () => controller.abort(); }, [load, generation]);
  useEffect(() => subscribe((event: LiveEvent) => { if (event.publicId !== publicId) return; if (event.type === "status") setHost((value) => value && ({ ...value, online: event.online })); else { setHost((value) => value && ({ ...value, online: true, latest: event.data })); setPoints((value) => [...value, event.data].filter((point) => point.timestamp >= event.data.timestamp - RANGES[key].seconds)); setWindowRange({ from: event.data.timestamp - RANGES[key].seconds, to: event.data.timestamp }); } }), [key, publicId, subscribe]);
  const timeline = useMemo(() => buildTimeline(points, windowRange.from, windowRange.to), [points, windowRange]);
  if (error) return <EmptyState title="主机详情加载失败" detail={error} action={<Link to="/" className="text-sm text-ink underline underline-offset-4">返回概览</Link>} />;
  if (!host) return <PageState message="正在加载主机详情…" />;
  const facts = [["主机名", host.hostname], ["系统", `${host.os} ${host.platform} ${host.platformVersion}`], ["架构", host.kernelArch], ["CPU", host.modelName], ["核心数", host.cores], ["虚拟化", host.virtualizationRole || "-"], ["HostID", host.hostId || "-"]];
  const charts = [{ title: "CPU", metric: "cpu" as const }, { title: `内存${host.latest?.memoryTotal ? ` · ${formatBytes(host.latest.memoryTotal)}` : ""}`, metric: "memory" as const }, { title: "网络", metric: "network" as const }, { title: "系统负载", metric: "load" as const }];
  return <section>
    <Link className="text-sm text-muted transition-colors hover:text-ink" to="/">← 返回概览</Link>
    <div className="mt-4 flex flex-wrap items-center justify-between gap-3 border-b border-line pb-5"><h1 className="font-serif text-[28px] font-semibold leading-tight tracking-tight">{host.nickname}</h1><StatusBadge online={host.online} /></div>
    <dl className="mt-6 overflow-hidden rounded-[10px] border border-line"><div className="-mb-px -mr-px grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-7">{facts.map(([label, value]) => <div key={label as string} className="border-b border-r border-line bg-surface px-3 py-3 shadow-[inset_0_1px_0_var(--edge-light)]"><dt className="text-xs text-muted">{label}</dt><dd className="mt-1 break-words font-mono text-sm tabular-nums">{String(value || "-")}</dd></div>)}</div></dl>
    <div className="my-5"><TabBar label="图表时间范围">{(Object.keys(RANGES) as RangeKey[]).map((range) => <Tab key={range} active={range === key} onClick={() => setParams({ range })}>{RANGES[range].label}</Tab>)}</TabBar></div>
    <div className="grid gap-4 lg:grid-cols-2">{charts.map((chart) => <MetricChart key={chart.metric} {...chart} timeline={timeline} from={windowRange.from} to={windowRange.to} onOpen={() => setZoom(chart)} />)}</div>
    {zoom && <ChartDialog title={zoom.title} onClose={() => setZoom(null)}><Line key={`${theme}-${revision}`} data={chartModel(timeline, zoom.metric)} options={makeChartOptions(zoom.metric, windowRange.from, windowRange.to)} /></ChartDialog>}
  </section>;
}
