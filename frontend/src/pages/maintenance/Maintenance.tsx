import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../../api";
import type { AgentReleases, Host, HostMaintenance, MaintenanceEvent, MaintenanceKind, MaintenanceStatus, MaintenanceTask } from "../../api/types";
import { useApp } from "../../app/AppContext";
import { Button, ConfirmDialog, EmptyState, PageState, fieldClass, zoneClass } from "../../components/ui";
import { formatDateTime } from "../../utils/format";
import { binaryAsset, compareVersions, releaseSupports } from "../../utils/releases";

const statusLabels: Record<MaintenanceStatus, string> = { queued: "排队中", delivered: "已发起投递，等待确认", accepted: "已接受", running: "执行中", verifying: "验证中", succeeded: "成功", failed: "失败", unknown: "状态未知", expired: "已过期" };
const statusTones: Record<MaintenanceStatus, string> = { queued: "text-muted", delivered: "text-muted", accepted: "text-muted", running: "text-muted", verifying: "text-muted", succeeded: "text-success", failed: "text-danger", unknown: "text-warning", expired: "text-offline" };
const kindLabels: Record<MaintenanceKind, string> = { agent_upgrade: "升级 Agent", system_update: "更新系统软件包" };

const isoTime = (seconds: number) => new Date(seconds * 1000).toISOString();

function TaskStatus({ status }: { status: MaintenanceStatus }) {
  return <span className={`inline-flex items-center gap-1.5 text-xs ${statusTones[status]}`}><span className="h-1.5 w-1.5 rounded-full bg-current" aria-hidden="true" />{statusLabels[status]}</span>;
}

function usePolling(load: (signal: AbortSignal) => Promise<number>) {
  const [error, setError] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    const run = async () => {
      let delay = 5000;
      try { delay = await load(controller.signal); if (!controller.signal.aborted) setError(""); }
      catch { if (!controller.signal.aborted) setError("加载失败，将自动重试读取。"); }
      if (!controller.signal.aborted && delay >= 0) timer = setTimeout(run, delay);
    };
    void run();
    return () => { controller.abort(); clearTimeout(timer); };
  }, [load]);
  return error;
}

function TaskDetail({ id }: { id: string }) {
  const [task, setTask] = useState<MaintenanceTask>();
  const [events, setEvents] = useState<MaintenanceEvent[]>([]);
  const after = useRef(0);
  const error = usePolling(useCallback(async (signal: AbortSignal) => {
    const result = await api.maintenanceDetail(id, after.current, signal);
    if (signal.aborted) return -1;
    setTask(result.task);
    setEvents(previous => [...previous, ...result.events.filter(event => !previous.some(old => old.seq === event.seq))]);
    const advanced = result.nextAfter > after.current;
    after.current = result.nextAfter;
    return result.hasMore && advanced ? 0 : 5000;
  }, [id]));
  return <section aria-label="任务详情" className={zoneClass}>
    <h3 className="flex flex-wrap items-center gap-2 text-sm font-semibold">任务 <span className="font-mono">{id}</span>{task && <>· <TaskStatus status={task.status} /></>}</h3>
    {task && <p className="text-sm text-muted">创建者 {task.createdBy} · 更新 <time dateTime={isoTime(task.updatedAt)} className="font-mono text-xs">{formatDateTime(task.updatedAt)}</time></p>}
    {error && <p role="alert" className="text-sm text-danger">{error}</p>}
    <pre className="max-h-96 overflow-auto whitespace-pre-wrap break-words rounded-md bg-surface-muted p-3 font-mono text-xs">{events.map(event => <span key={event.seq}>{formatDateTime(event.createdAt)} [{statusLabels[event.status]}] {event.message}{"\n"}</span>)}</pre>
  </section>;
}

function TaskList({ tasks, onSelect }: { tasks: MaintenanceTask[]; onSelect: (id: string) => void }) {
  return <ul className="divide-y divide-line overflow-hidden rounded-[10px] border border-line">{tasks.map(task => <li key={task.id} className="flex min-h-14 flex-wrap items-center justify-between gap-x-4 gap-y-2 bg-surface px-4 py-3 shadow-[inset_0_1px_0_var(--edge-light)]">
    <div className="min-w-0">
      <p className="text-sm"><span className="font-medium">{kindLabels[task.kind]}{task.targetVersion && <> <span className="font-mono">{task.targetVersion}</span></>}</span><span className="text-faint"> · </span><span className="font-mono text-xs text-muted">{task.publicId}</span></p>
      <p className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1"><TaskStatus status={task.status} /><time dateTime={isoTime(task.createdAt)} className="font-mono text-xs text-faint">{formatDateTime(task.createdAt)}</time></p>
    </div>
    <Button size="sm" className="shrink-0" onClick={() => onSelect(task.id)}>查看任务 {task.id}</Button>
  </li>)}</ul>;
}

function MaintenanceList() {
  const [before, setBefore] = useState<number>();
  return <section className="space-y-4">
    <h2 className="text-lg font-semibold">维护任务</h2>
    <p className="text-sm text-muted">查看各主机维护任务的状态、阶段和日志；新任务在主机详情页发起。</p>
    {before !== undefined && <Button onClick={() => setBefore(undefined)}>最新任务</Button>}
    <TaskPage key={before ?? "latest"} before={before} onNext={setBefore} />
  </section>;
}

function TaskPage({ before, onNext }: { before?: number; onNext: (before: number) => void }) {
  const [tasks, setTasks] = useState<MaintenanceTask[]>([]);
  const [next, setNext] = useState<number>();
  const [selected, setSelected] = useState("");
  const [loaded, setLoaded] = useState(false);
  const error = usePolling(useCallback(async (signal: AbortSignal) => {
    const result = await api.maintenance(undefined, before, signal);
    if (!signal.aborted) { setTasks(result.tasks); setNext(result.nextBefore); setLoaded(true); }
    return before === undefined ? 5000 : -1;
  }, [before]));
  return <div className="space-y-4">
    {error && <p role="alert" className="text-sm text-danger">{error}</p>}
    {!loaded && !error && <PageState message="正在加载维护任务…" />}
    {loaded && !tasks.length && <EmptyState title="暂无维护任务" detail="从主机详情页发起 Agent 升级或系统软件包更新后，任务会登记在这里。" />}
    {tasks.length > 0 && <TaskList tasks={tasks} onSelect={setSelected} />}
    {next !== undefined && <Button onClick={() => onNext(next)}>下一页</Button>}
    {selected && <TaskDetail key={selected} id={selected} />}
  </div>;
}

export function MaintenancePage() {
  const { auth } = useApp();
  return auth.loggedIn ? <MaintenanceList /> : <p className="text-sm text-muted">请登录后查看维护任务。</p>;
}

export function HostMaintenancePanel({ host }: { host: Host }) {
  const { auth } = useApp();
  return auth.loggedIn ? <HostControls key={host.publicId} host={host} /> : null;
}

function HostControls({ host }: { host: Host }) {
  const [state, setState] = useState<HostMaintenance>();
  const [releases, setReleases] = useState<AgentReleases>();
  const [releaseError, setReleaseError] = useState("");
  const [version, setVersion] = useState("");
  const [confirm, setConfirm] = useState<MaintenanceKind>();
  const [selected, setSelected] = useState("");
  const [pending, setPending] = useState(false);
  const [uncertain, setUncertain] = useState(false);
  const submitting = useRef(false);
  const revision = useRef(0);
  const request = useRef<AbortController | null>(null);
  useEffect(() => () => request.current?.abort(), []);
  const error = usePolling(useCallback(async (signal: AbortSignal) => {
    const started = revision.current;
    try {
      const result = await api.hostMaintenance(host.publicId, signal);
      if (!signal.aborted && started === revision.current && !submitting.current) setState(result);
    } catch (error) { if (!signal.aborted) setState(previous => previous ? { ...previous, supported: false, online: false } : previous); throw error; }
    return 5000;
  }, [host.publicId]));
  useEffect(() => {
    const controller = new AbortController();
    api.agentReleases(controller.signal).then(result => { if (!controller.signal.aborted) setReleases(result); }).catch(() => { if (!controller.signal.aborted) setReleaseError("发布信息加载失败，无法升级 Agent。"); });
    return () => controller.abort();
  }, []);
  const binary = binaryAsset(host);
  const versions = releases && !releases.stale && !releases.error && binary ? releases.releases.filter(release => releaseSupports(release, "upgrade", binary) && compareVersions(release.version, host.agentVersion ?? "") === 1).map(release => release.version) : [];
  const target = versions.includes(version) ? version : versions[0] ?? "";
  const blocked = !state?.supported || !state.online || !host.online || state.tasks.some(task => !["succeeded", "failed", "expired"].includes(task.status));
  const disabled = blocked || pending || uncertain;
  const create = async () => {
    const kind = confirm;
    setConfirm(undefined);
    if (!kind || disabled || submitting.current || (kind === "agent_upgrade" && !target)) return;
    submitting.current = true;
    revision.current++;
    setPending(true);
    const controller = new AbortController();
    request.current = controller;
    try {
      const task = await api.createMaintenance(host.publicId, { kind, ...(kind === "agent_upgrade" ? { targetVersion: target } : {}) }, controller.signal);
      if (!controller.signal.aborted) { setSelected(task.id); setState(previous => previous ? { ...previous, tasks: [task, ...previous.tasks.filter(old => old.id !== task.id)] } : previous); }
    } catch { if (!controller.signal.aborted) setUncertain(true); }
    finally { if (!controller.signal.aborted) { revision.current++; setPending(false); submitting.current = false; } }
  };
  return <section className={`my-6 ${zoneClass}`} aria-label="主机维护">
    <h2 className="text-lg font-semibold">主机维护</h2>
    <p className="text-sm text-muted">仅支持在线且具备维护能力的主机；进行中或状态未知的任务会阻止新任务。</p>
    {!state && !error && <p className="text-sm text-muted">正在确认主机维护能力…</p>}
    {error ? <p role="alert" className="text-sm text-danger">{error} 主机维护状态未确认，暂不可创建任务。</p> : state && (!state.supported ? <p className="text-sm text-muted">此主机不支持远程维护。</p> : (!state.online || !host.online) && <p className="text-sm text-muted">主机离线，暂不可创建维护任务。</p>)}
    {releases?.stale && <p role="alert" className="text-sm text-danger">发布信息已过期，暂不可升级 Agent。</p>}
    {releases?.error && <p role="alert" className="text-sm text-danger">发布信息异常：{releases.error}</p>}
    {releaseError && <p role="alert" className="text-sm text-danger">{releaseError}</p>}
    {uncertain && <p role="alert" className="text-sm text-danger">请求结果不确定，任务可能已创建。不会自动重试；请先核对任务记录和主机状态，勿重复提交。</p>}
    <div className="flex flex-wrap items-end gap-2">
      <label className="grid w-56 max-w-full gap-1 text-sm text-muted">Agent 目标版本
        <select className={`${fieldClass} pr-9 font-mono`} value={target} disabled={disabled || !target} onChange={event => setVersion(event.target.value)}>
          {!target && <option value="">无可用的更新正式版</option>}
          {versions.map(value => <option key={value}>{value}</option>)}
        </select>
      </label>
      <Button disabled={disabled || !target} onClick={() => setConfirm("agent_upgrade")}>升级 Agent</Button>
      <Button disabled={disabled} onClick={() => setConfirm("system_update")}>更新系统软件包</Button>
    </div>
    {(state?.tasks.length ?? 0) > 0 && <TaskList tasks={state?.tasks ?? []} onSelect={setSelected} />}
    {selected && <TaskDetail key={selected} id={selected} />}
    {confirm && <ConfirmDialog title={confirm === "agent_upgrade" ? "确认升级 Agent" : "确认更新系统软件包"} detail={"主机：" + (host.nickname || host.publicId) + (confirm === "agent_upgrade" ? "；目标版本：" + target : "；更新已安装的软件包") + "。可能中断服务；不会自动重启主机，不会执行发行版升级（dist-upgrade）。"} confirmLabel="确认执行" onClose={() => setConfirm(undefined)} onConfirm={() => void create()} />}
  </section>;
}
