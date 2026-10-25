import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { NavLink, Navigate, Outlet, useOutletContext } from "react-router-dom";
import { api } from "../../api";
import type { AgentReleases, Host, HostEnrollment, NotificationRule, Tag } from "../../api/types";
import { Button, ConfirmDialog, EmptyState, PageState, StatusBadge, fieldClass } from "../../components/ui";
import { useApp } from "../../app/AppContext";
import { enrollmentCommand, isGithubProxy, upgradeCommand } from "../../utils/enrollment";
import { binaryAsset, compareVersions, releaseSupports, stableRelease, versionStatus } from "../../utils/releases";
import { copyText } from "../../utils/clipboard";
import { errorMessage, formatDateTime } from "../../utils/format";

type AdminContext = { refresh: number; invalidate: () => void };
const useAdmin = () => useOutletContext<AdminContext>();
const panels = [["hosts", "主机管理"], ["tags", "标签管理"], ["settings", "网站设置"], ["notifications", "状态通知"]];
const navLinkClass = ({ isActive }: { isActive: boolean }) => `block min-h-11 whitespace-nowrap border-b-2 px-3 py-2.5 text-sm transition-colors lg:border-b-0 lg:border-l-2 ${isActive ? "border-ink font-medium text-ink" : "border-transparent text-muted hover:text-ink"}`;
const zoneClass = "grid gap-4 rounded-[10px] border border-line bg-surface p-4 shadow-[0_1px_2px_rgba(0,0,0,0.04),inset_0_1px_0_var(--edge-light)]";

export function AdminPage() {
  const { auth } = useApp(); const [refresh, setRefresh] = useState(0);
  if (!auth.loggedIn) return <Navigate to="/unauthorized" replace />;
  return <section>
    <div className="mb-6 border-b border-line pb-5"><p className="font-mono text-[11px] uppercase tracking-[0.16em] text-faint">Administration</p><h1 className="mt-2 font-serif text-[28px] font-semibold leading-tight tracking-tight">后台管理</h1><p className="mt-2 font-mono text-xs text-muted">Monitor 版本：{auth.monitorVersion?.trim() || "未知"}</p></div>
    <div className="grid gap-6 lg:grid-cols-[180px_minmax(0,1fr)]">
      <nav className="flex gap-4 overflow-x-auto border-b border-line lg:block lg:space-y-1 lg:border-b-0 lg:border-r lg:pr-5" aria-label="后台管理">{panels.map(([path, label]) => <NavLink key={path} to={`/admin/${path}`} className={navLinkClass}>{label}</NavLink>)}</nav>
      <div className="min-w-0"><Outlet context={{ refresh, invalidate: () => setRefresh((value) => value + 1) }} /></div>
    </div>
  </section>;
}

function FieldMessage({ value, success = false }: { value: string; success?: boolean }) { return value ? <p aria-live="polite" className={`text-sm ${success ? "text-success" : "text-danger"}`}>{value}</p> : null; }
function CheckTags({ tags, value, onChange }: { tags: Tag[]; value: string[]; onChange: (value: string[]) => void }) { return <div className="flex flex-wrap gap-3">{tags.map((tag) => <label key={tag.id} className="flex min-h-11 items-center gap-2 text-sm"><input type="checkbox" checked={value.includes(tag.name)} onChange={(event) => onChange(event.target.checked ? [...value, tag.name] : value.filter((item) => item !== tag.name))} />{tag.name}</label>)}</div>; }

export function HostsPanel() {
  const { refresh, invalidate } = useAdmin(); const [hosts, setHosts] = useState<Host[]>(); const [tags, setTags] = useState<Tag[]>([]); const [error, setError] = useState(""); const [copyMessage, setCopyMessage] = useState(""); const [nickname, setNickname] = useState(""); const [selected, setSelected] = useState<string[]>([]); const [created, setCreated] = useState<HostEnrollment>(); const [busy, setBusy] = useState(false); const [now, setNow] = useState(Date.now()); const [editing, setEditing] = useState<Host>(); const [deleting, setDeleting] = useState<Host>();
  const [agentRelease, setAgentRelease] = useState("");
  const releaseChosen = useRef(false);
  const [hostLoadError, setHostLoadError] = useState("");
  const [releases, setReleases] = useState<AgentReleases>();
  const [releaseError, setReleaseError] = useState("");
  const [releaseLoading, setReleaseLoading] = useState(true);
  const [releaseRefresh, setReleaseRefresh] = useState(0);
  const [proxy, setProxy] = useState("");
  const [upgrading, setUpgrading] = useState("");
  useEffect(() => {
    const controller = new AbortController(); let active = true; let timer: number | undefined;
    const fetchReleases = async () => {
      setReleaseLoading(true);
      try { const data = await api.agentReleases(controller.signal); if (!active) return; setReleases(data); setReleaseError(data.error || ""); setAgentRelease((current) => releaseChosen.current ? current : data.releases.find((item) => releaseSupports(item, "install"))?.version || ""); }
      catch (reason) { if (active) setReleaseError(errorMessage(reason)); }
      finally { if (active) { setReleaseLoading(false); timer = window.setTimeout(() => void fetchReleases(), 600000); } }
    };
    void fetchReleases();
    return () => { active = false; controller.abort(); window.clearTimeout(timer); };
  }, [releaseRefresh]);
  const target = releases?.releases.find((item) => item.version === agentRelease);
  const proxySupported = !!target?.assets.includes("upgrade.sh");
  const activeProxy = proxySupported ? proxy : "";
  const proxyValid = isGithubProxy(activeProxy);
  const hostRequest = useRef(0);
  const hostLoadActive = useRef(false);
  const load = useCallback(async () => {
    if (!hostLoadActive.current) return;
    const request = ++hostRequest.current;
    try { const [nextHosts, nextTags] = await Promise.all([api.hosts(), api.tags()]); if (!hostLoadActive.current || request !== hostRequest.current) return; setHosts(nextHosts); setTags(nextTags); setHostLoadError(""); }
    catch (reason) { if (hostLoadActive.current && request === hostRequest.current) setHostLoadError(errorMessage(reason)); }
  }, []);
  useEffect(() => { hostLoadActive.current = true; void load(); const timer = window.setInterval(() => void load(), 600000); return () => { hostLoadActive.current = false; ++hostRequest.current; window.clearInterval(timer); }; }, [load, refresh]);
  useEffect(() => { const timer = window.setInterval(() => setNow(Date.now()), 1000); return () => window.clearInterval(timer); }, []);
  useEffect(() => { if (created && hosts?.some((host) => host.publicId === created.publicId && host.publicKey)) setCreated(undefined); }, [hosts, created]);
  const regenerate = async (host: Host) => { setBusy(true); setError(""); setCreated(undefined); setCopyMessage(""); try { setCreated(await api.refreshEnrollment(host.publicId)); setNow(Date.now()); await load(); } catch (reason) { setError(errorMessage(reason)); } finally { setBusy(false); } };
  const create = async (event: FormEvent) => { event.preventDefault(); setError(""); setBusy(true); setCreated(undefined); setCopyMessage(""); try { const host = await api.createHost({ nickname, tags: selected }); setNow(Date.now()); setCreated(host); setNickname(""); setSelected([]); await load(); invalidate(); } catch (reason) { setError(errorMessage(reason)); } finally { setBusy(false); } };
  const save = async () => { if (!editing) return; try { await api.updateHost(editing.publicId, { nickname: editing.nickname, tags: editing.tags }); setEditing(undefined); await load(); invalidate(); } catch (reason) { setError(errorMessage(reason)); } };
  const remove = async () => { if (!deleting) return; try { await api.deleteHost(deleting.publicId); if (created?.publicId === deleting.publicId) setCreated(undefined); setDeleting(undefined); await load(); invalidate(); } catch (reason) { setError(errorMessage(reason)); } };
  const loopback = location.hostname === "localhost" || location.hostname === "[::1]" || /^127\.\d+\.\d+\.\d+$/.test(location.hostname);
  const reportUrl = location.protocol === "https:" ? `wss://${location.host}/report` : location.protocol === "http:" && loopback ? `ws://${location.host}/report` : undefined;
  const enrollmentHost = created && hosts?.find((host) => host.publicId === created.publicId);
  const enrollmentPlatformKnown = !!(enrollmentHost?.os || enrollmentHost?.kernelArch);
  const enrollmentBinary = enrollmentHost && binaryAsset(enrollmentHost);
  const enrollmentPlatformSupported = !enrollmentPlatformKnown || !!enrollmentBinary;
  const expired = !!created && now >= created.enrollmentExpiresAt * 1000;
  const command = created && !expired && reportUrl && proxyValid && enrollmentPlatformSupported && releaseSupports(target, "install", enrollmentBinary) ? enrollmentCommand(created.enrollmentToken, reportUrl, agentRelease, false, activeProxy) : undefined;
  const migrationCommand = created && !expired && reportUrl && proxyValid && enrollmentPlatformSupported && releaseSupports(target, "migrate-token-to-key", enrollmentBinary) ? enrollmentCommand(created.enrollmentToken, reportUrl, agentRelease, true, activeProxy) : undefined;
  if (!hosts && hostLoadError) return <PageState label="ERROR" tone="danger" message={`主机管理加载失败：${hostLoadError}`} action={<Button onClick={() => { setError(""); void load(); }}>重新加载</Button>} />;
  if (!hosts) return <PageState message="正在加载主机管理…" />;
  return <div>
    <h2 className="text-lg font-semibold">主机管理</h2>
    <p className="mt-4 text-sm text-muted">添加昵称与标签后，复制短期安装命令到 Agent 主机执行，即可自动生成密钥、注册绑定、写入配置并启动，无需手抄公钥或审批。</p>
    <form onSubmit={create} className={`mt-4 ${zoneClass}`}><div className="grid gap-3 md:grid-cols-2"><label className="text-sm text-muted">昵称<input className={`${fieldClass} mt-1`} value={nickname} onChange={(event) => setNickname(event.target.value)} required /></label></div><div><span className="text-sm text-muted">标签</span><CheckTags tags={tags} value={selected} onChange={setSelected} /></div><div><Button variant="primary" type="submit" disabled={busy}>添加主机</Button></div><FieldMessage value={error} /><FieldMessage value={hostLoadError} /></form>
    <div className={`mt-4 ${zoneClass}`}>
      <p role="status" className="text-sm text-muted">{releaseLoading ? "正在加载发布列表…" : releaseError ? `发布列表加载失败：${releaseError}` : releases?.stale ? "发布列表已过期（缓存），无法确认最新版。" : "发布列表已更新，每 10 分钟自动刷新。"}{releases?.checkedAt ? ` 上次检查：${formatDateTime(releases.checkedAt)}` : ""}</p>
      {(releaseError || releases?.stale) && <p className="text-sm text-danger">{releases?.releases.length ? "仅供参考：使用缓存发布列表，无法确认是否最新。" : "发布列表不可用，无法生成命令。"}</p>}
      <div><Button size="sm" disabled={releaseLoading} onClick={() => setReleaseRefresh((value) => value + 1)}>重试发布列表</Button></div>
      {!releaseLoading && !releaseError && !releases?.releases.length && <p className="text-sm text-muted">暂无正式发布版本。</p>}
      <label className="text-sm text-muted">目标 Agent release<span className="relative mt-1 block"><select aria-label="目标 Agent release" className={`${fieldClass} appearance-none pr-9 font-mono`} value={agentRelease} onChange={(event) => { releaseChosen.current = true; setAgentRelease(event.target.value); setCopyMessage(""); setUpgrading(""); }}><option value="">选择正式发布版本</option>{agentRelease && !target && <option value={agentRelease} disabled>{agentRelease}（已不可用，请重新选择）</option>}{releases?.releases.filter((item) => stableRelease(item.version)).map((item) => <option key={item.version} value={item.version}>{item.version}</option>)}</select><span aria-hidden="true" className="pointer-events-none absolute right-3 top-2 text-muted">⌄</span></span></label>
      {target && !releaseSupports(target, "install") && <p className="text-sm text-muted">目标版本缺少新安装所需资产，请选择其他版本或手动安装。</p>}
      <label className="text-sm text-muted">GitHub HTTPS 代理前缀（可选）<input className={`${fieldClass} mt-1 font-mono`} value={proxy} disabled={!proxySupported} aria-invalid={!proxyValid} onChange={(event) => { setProxy(event.target.value); setCopyMessage(""); }} /></label>
      {!proxySupported && <p className="text-sm text-muted">此版本无 upgrade.sh，代理参数不受支持，已禁用代理；使用直连，不发送未知参数。</p>}
      {!proxyValid && <p className="text-sm text-danger">代理必须为 HTTPS ASCII 主机、可选端口与安全路径；不支持凭据、查询、片段、空白、控制字符、百分号编码、IPv6 或重复斜线。</p>}
      <p className="text-sm text-muted">代理会同时提供脚本、二进制和 checksums；校验和不构成独立来源认证，仅使用可信代理。</p>
    </div>
    {created && <div className="mt-4 rounded-[10px] border border-dashed border-line-strong bg-surface p-4">
      <p className="text-sm font-medium">{created.nickname}：一次性注册凭据仅本次显示</p>
      <p className="mt-2 text-sm text-muted">有效期 15 分钟，过期时间：{formatDateTime(created.enrollmentExpiresAt)}。刷新页面无法找回明文；重新生成会立即使旧凭据失效。</p>
      <p className="mt-2 text-sm text-danger">请保密：持有凭据者可抢先绑定主机，无额外审批。不要分享命令、截图或日志；命令可能留在终端历史和剪贴板中。</p>
      <p className="mt-2 text-sm text-muted">从发布列表选择目标版本，默认选择最新可安装正式版。仅支持 Linux x86_64 / aarch64；其他系统或架构请手动安装。命令自动通过 HTTPS 下载该固定版本的 bootstrap 和安装资源，无需手动下载二进制。需要 bash、curl 及 root 权限（非 root 使用 sudo）。</p>
      <p className="mt-2 text-sm text-muted">待注册只表示尚未绑定公钥，不代表机器是新装还是旧版。未安装请选择“新安装”；已有 v0.0.2 Token 标准安装请选择“迁移旧 Token Agent（v0.0.2）”。请在原主机记录上重新生成凭据以保留历史，不要删除重建。两条命令共用同一凭据，只执行适合的一条；成功绑定后，另一条仅能使用同一私钥恢复，不能再次注册新钥。</p>
      {!enrollmentPlatformSupported && <p className="mt-2 text-sm text-danger">此主机的 OS / 架构不受支持或信息不完整，请手动安装或迁移。</p>}
      {target && !releaseSupports(target, "migrate-token-to-key", enrollmentBinary) && <p className="mt-2 text-sm text-muted">目标版本缺少迁移所需资产，不能生成迁移命令。</p>}
      {expired ? <p role="status" className="mt-3 text-sm text-danger">注册凭据已过期，请在待注册主机列表重新生成凭据。</p> : [["新安装", command], ["迁移旧 Token Agent（v0.0.2）", migrationCommand]].filter((item): item is [string, string] => !!item[1]).map(([label, value]) => <div key={label} className="mt-3 grid gap-2"><span className="text-sm font-medium">{label}</span><code className="break-all rounded-md bg-surface-muted p-2 font-mono text-xs">{value}</code><Button size="sm" onClick={() => { if (Date.now() >= created.enrollmentExpiresAt * 1000) { setNow(Date.now()); return; } void copyText(value).then(() => setCopyMessage(`${label}已复制`)).catch((reason) => setError(errorMessage(reason))); }}>复制{label}</Button></div>)}
      {migrationCommand && <details className="mt-3 text-sm text-muted"><summary className="cursor-pointer text-ink">迁移限制与回滚说明</summary>
<p className="mt-4 text-sm text-muted">此独立脚本只处理 v0.0.2 Token 到公钥的大 breaking 迁移，不支持常规版本更新，已公钥安装不能借此升级。正常成功后重跑仅确认迁移完成，不更换版本。迁移保留采集配置和私钥，执行前仍须备份以便本地回滚。脚本仅支持标准安装，非标准服务安装会被拒绝，不保证保留未知服务定制。失败可恢复本地备份，但服务端已完成的公钥绑定不能回滚；请保留新生成的私钥以便重试。旧 Token 已不被新版 Monitor 接受。</p>
      </details>}
      <p className="mt-3 text-sm text-muted">生产环境必须配置实际 WSS 地址并验证服务端证书；仅 loopback 本机开发可使用 WS，且 Agent 必须显式开启 devMode: true。</p>
      {!reportUrl && <p className="mt-2 text-sm text-danger">当前页面不是 HTTPS 或 loopback HTTP，无法生成安全的 Report URL。请使用部署的实际 WSS 地址。</p>}
      <FieldMessage value={copyMessage} success />
    </div>}
    <div className="mt-8"><div className="mb-2 flex items-center justify-between"><h3 className="text-sm font-semibold">主机列表</h3><Button size="sm" onClick={() => void load()}>刷新主机版本</Button></div>{!hosts.length ? <EmptyState title="暂无主机" detail="使用上方表单添加第一台主机。" /> : <div className="divide-y divide-line overflow-hidden rounded-[10px] border border-line">{hosts.map((host) => { const current = editing?.publicId === host.publicId ? editing : undefined; const binary = binaryAsset(host); const order = compareVersions(host.agentVersion || "", agentRelease); const canUpgrade = !!host.publicKey && !!binary && releaseSupports(target, "upgrade", binary) && order !== undefined && order < 0; const upgrade = canUpgrade && proxyValid ? upgradeCommand(agentRelease, activeProxy) : undefined; return <article key={host.publicId} className="grid gap-3 bg-surface px-4 py-3 shadow-[inset_0_1px_0_var(--edge-light)] lg:grid-cols-[minmax(240px,1.3fr)_minmax(220px,1fr)_auto] lg:items-center"><div className="min-w-0">{current ? <input className={fieldClass} value={current.nickname} onChange={(event) => setEditing({ ...current, nickname: event.target.value })} /> : <div className="flex items-center gap-2.5"><p className="truncate text-sm font-medium">{host.nickname}</p><StatusBadge online={host.online} /></div>}<p className="mt-1 break-all font-mono text-xs text-faint">{host.publicId}</p><p className="mt-1 font-mono text-xs text-muted">Agent {host.agentVersion || "未知"} · {versionStatus(host.agentVersion || "", releases?.latestVersion || "", releaseLoading || !!releaseError || !!releases?.stale || !!hostLoadError)}</p>{host.publicKey && !binary && <p className="mt-1 text-xs text-muted">不支持的 OS / 架构（{host.os || "未知"} / {host.kernelArch || "未知"}），请手动升级。</p>}{host.publicKey && binary && order !== undefined && order < 0 && !releaseSupports(target, "upgrade", binary) && <p className="mt-1 text-xs text-muted">目标版本缺少此架构升级资产，请选择其他版本或手动升级。</p>}{!host.publicKey && <p className="mt-1 text-xs text-danger">待注册：尚未绑定公钥。</p>}</div><div>{current ? <CheckTags tags={tags} value={current.tags} onChange={(value) => setEditing({ ...current, tags: value })} /> : host.tags.length ? <div className="flex flex-wrap gap-1">{host.tags.map((tag) => <span key={tag} className="rounded-full border border-line px-2.5 py-1 text-xs text-muted">{tag}</span>)}</div> : <span className="text-xs text-faint">未分组</span>}</div><div className="flex flex-wrap items-center justify-end gap-2">{current ? <><Button size="sm" variant="primary" onClick={() => void save()}>保存</Button><Button size="sm" onClick={() => setEditing(undefined)}>取消</Button></> : <><Button size="sm" onClick={() => setEditing({ ...host, tags: [...host.tags] })}>编辑</Button>{canUpgrade && <Button size="sm" disabled={!proxyValid} aria-expanded={upgrading === host.publicId} onClick={() => setUpgrading(upgrading === host.publicId ? "" : host.publicId)}>升级 Agent</Button>}{!host.publicKey && <Button size="sm" disabled={busy} onClick={() => void regenerate(host)}>重新生成凭据</Button>}<Button size="sm" variant="ghost" className="text-danger" onClick={() => setDeleting(host)}>删除</Button></>}</div>{upgrading === host.publicId && upgrade && <div className="grid gap-2 border-t border-line pt-3 lg:col-span-3"><p className="text-sm text-muted">升级至 {agentRelease}，无需注册凭据，不更换私钥。仅支持标准安装，执行前请备份。</p><code className="break-all rounded-md bg-surface-muted p-2 font-mono text-xs">{upgrade}</code><div><Button size="sm" onClick={() => void copyText(upgrade).then(() => setCopyMessage("升级命令已复制")).catch((reason) => setError(errorMessage(reason)))}>复制升级命令</Button></div><FieldMessage value={copyMessage} success /></div>}</article>; })}</div>}</div>
    {deleting && <ConfirmDialog title="删除主机" detail={`将删除“${deleting.nickname}”及其全部历史数据。此操作无法撤销。`} onClose={() => setDeleting(undefined)} onConfirm={() => void remove()} />}
  </div>;
}

export function TagsPanel() {
  const { refresh, invalidate } = useAdmin(); const [tags, setTags] = useState<Tag[]>(); const [hosts, setHosts] = useState<Host[]>([]); const [name, setName] = useState(""); const [editing, setEditing] = useState<Tag>(); const [deleting, setDeleting] = useState<Tag>(); const [message, setMessage] = useState("");
  const load = useCallback(() => Promise.all([api.tags(), api.hosts()]).then(([nextTags, nextHosts]) => { setTags(nextTags); setHosts(nextHosts); }).catch((reason) => setMessage(errorMessage(reason))), []); useEffect(() => { void load(); }, [load, refresh]);
  const countOf = (tagName: string) => hosts.filter((host) => host.tags.includes(tagName)).length;
  const submit = async (event: FormEvent) => { event.preventDefault(); try { if (editing) await api.updateTag(editing.id, name); else await api.createTag(name); setName(""); setEditing(undefined); setMessage(""); await load(); invalidate(); } catch (reason) { setMessage(errorMessage(reason)); } };
  const remove = async () => { if (!deleting) return; try { await api.deleteTag(deleting.id); setDeleting(undefined); await load(); invalidate(); } catch (reason) { setMessage(errorMessage(reason)); } };
  if (!tags && message) return <PageState label="ERROR" tone="danger" message={`标签加载失败：${message}`} action={<Button onClick={() => { setMessage(""); void load(); }}>重新加载</Button>} />;
  if (!tags) return <PageState message="正在加载标签…" />;
  return <div>
    <h2 className="text-lg font-semibold">标签管理</h2>
    <p className="mt-1 text-sm text-muted">先创建标签，再分配给主机或用于通知匹配。</p>
    <form onSubmit={submit} className={`mt-4 max-w-xl ${zoneClass}`}><label className="text-sm text-muted">标签名称<input className={`${fieldClass} mt-1`} maxLength={40} value={name} onChange={(event) => setName(event.target.value)} placeholder="例如 prod" required /></label><div className="flex gap-2"><Button variant="primary" type="submit">{editing ? "保存标签" : "添加标签"}</Button>{editing && <Button onClick={() => { setEditing(undefined); setName(""); }}>取消</Button>}</div><FieldMessage value={message} /></form>
    <div className="mt-8 divide-y divide-line overflow-hidden rounded-[10px] border border-line">{tags.map((tag) => <div key={tag.id} className="flex min-h-14 items-center justify-between gap-4 bg-surface px-4 py-3 shadow-[inset_0_1px_0_var(--edge-light)]"><div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1"><span className="rounded-full border border-line px-2.5 py-1 text-xs text-muted">{tag.name}</span><span className="font-mono text-xs text-faint">{countOf(tag.name)} 台主机 · {formatDateTime(tag.createdAt)}</span></div><div className="flex shrink-0 gap-2"><Button size="sm" onClick={() => { setEditing(tag); setName(tag.name); }}>编辑</Button><Button size="sm" variant="ghost" className="text-danger" onClick={() => setDeleting(tag)}>删除</Button></div></div>)}</div>
    {!tags.length && <EmptyState title="暂无标签" detail="添加标签后即可整理主机和匹配通知。" />}
    {deleting && <ConfirmDialog title="删除标签" detail={`“${deleting.name}”将从所有主机移除。`} onClose={() => setDeleting(undefined)} onConfirm={() => void remove()} />}
  </div>;
}

export function SettingsPanel() {
  const { settings, setSettings } = useApp(); const [draft, setDraft] = useState(settings); const [message, setMessage] = useState(""); const [success, setSuccess] = useState(false);
  const submit = async (event: FormEvent) => { event.preventDefault(); try { const saved = await api.updateSettings(draft); setSettings(saved); setDraft(saved); setSuccess(true); setMessage("设置已保存"); } catch (reason) { setSuccess(false); setMessage(errorMessage(reason)); } };
  return <div>
    <h2 className="text-lg font-semibold">网站设置</h2>
    <form onSubmit={submit} className={`mt-4 max-w-2xl ${zoneClass}`}><label className="text-sm text-muted">网站名称<input className={`${fieldClass} mt-1`} maxLength={60} value={draft.siteName} onChange={(event) => setDraft({ ...draft, siteName: event.target.value })} required /></label><label className="text-sm text-muted">网站描述<textarea className={`${fieldClass} mt-1 min-h-28 resize-y`} maxLength={200} value={draft.siteDescription} onChange={(event) => setDraft({ ...draft, siteDescription: event.target.value })} /></label><div><Button variant="primary" type="submit">保存设置</Button></div><FieldMessage value={message} success={success} /></form>
  </div>;
}

const emptyRule = (): Omit<NotificationRule, "id" | "createdAt"> => ({ tag: "", url: "", notifyOnline: true, notifyOffline: true, enabled: true });
export function NotificationsPanel() {
  const { refresh } = useAdmin(); const [rules, setRules] = useState<NotificationRule[]>(); const [tags, setTags] = useState<Tag[]>([]); const [draft, setDraft] = useState(emptyRule()); const [editing, setEditing] = useState<number>(); const [deleting, setDeleting] = useState<NotificationRule>(); const [message, setMessage] = useState("");
  const load = useCallback(() => Promise.all([api.notifications(), api.tags()]).then(([nextRules, nextTags]) => { setRules(nextRules); setTags(nextTags); }).catch((reason) => setMessage(errorMessage(reason))), []); useEffect(() => { void load(); }, [load, refresh]);
  const submit = async (event: FormEvent) => { event.preventDefault(); if (!draft.url.includes("%text%")) { setMessage("通知 URL 必须包含 %text%"); return; } if (!draft.notifyOnline && !draft.notifyOffline) { setMessage("至少选择一种状态事件"); return; } try { if (editing) await api.updateNotification(editing, draft); else await api.createNotification(draft); setDraft(emptyRule()); setEditing(undefined); setMessage(""); await load(); } catch (reason) { setMessage(errorMessage(reason)); } };
  const remove = async () => { if (!deleting) return; try { await api.deleteNotification(deleting.id); setDeleting(undefined); await load(); } catch (reason) { setMessage(errorMessage(reason)); } };
  if (!rules && message) return <PageState label="ERROR" tone="danger" message={`通知规则加载失败：${message}`} action={<Button onClick={() => { setMessage(""); void load(); }}>重新加载</Button>} />;
  if (!rules) return <PageState message="正在加载通知规则…" />;
  return <div>
    <h2 className="text-lg font-semibold">状态通知</h2>
    <p className="mt-1 text-sm text-muted">标签留空表示全部主机。URL 必须包含 <code className="rounded-[2px] bg-surface-muted px-1 font-mono text-xs">%text%</code>。</p>
    <form onSubmit={submit} className={`mt-4 ${zoneClass}`}><div className="grid gap-3 md:grid-cols-[180px_1fr]"><label className="text-sm text-muted">匹配标签<select className={`${fieldClass} mt-1 pr-8`} value={draft.tag} onChange={(event) => setDraft({ ...draft, tag: event.target.value })}><option value="">全部主机</option>{tags.map((tag) => <option key={tag.id}>{tag.name}</option>)}</select></label><label className="text-sm text-muted">通知 URL<input className={`${fieldClass} mt-1 font-mono`} value={draft.url} onChange={(event) => setDraft({ ...draft, url: event.target.value })} placeholder="https://example.com/send?text=%text%" required /></label></div><div className="flex flex-wrap gap-5">{[["notifyOnline", "上线通知"], ["notifyOffline", "离线通知"], ["enabled", "启用规则"]].map(([key, label]) => <label key={key} className="flex min-h-11 items-center gap-2 text-sm"><input type="checkbox" checked={draft[key as keyof typeof draft] as boolean} onChange={(event) => setDraft({ ...draft, [key]: event.target.checked })} />{label}</label>)}</div><div className="flex gap-2"><Button variant="primary" type="submit">{editing ? "保存规则" : "添加规则"}</Button>{editing && <Button onClick={() => { setEditing(undefined); setDraft(emptyRule()); }}>取消</Button>}</div><FieldMessage value={message} /></form>
    <div className="mt-8 divide-y divide-line overflow-hidden rounded-[10px] border border-line">{rules.map((rule) => <article key={rule.id} className="grid gap-3 bg-surface px-4 py-3 shadow-[inset_0_1px_0_var(--edge-light)] lg:grid-cols-[120px_minmax(0,1fr)_160px_100px_auto] lg:items-center"><p className="text-sm">{rule.tag || "全部主机"}</p><code className="truncate font-mono text-xs text-faint" title={rule.url}>{rule.url}</code><p className="text-sm text-muted">{[rule.notifyOnline && "上线", rule.notifyOffline && "离线"].filter(Boolean).join(" · ")}</p><p className={`text-sm ${rule.enabled ? "text-success" : "text-muted"}`}>{rule.enabled ? "已启用" : "已停用"}</p><div className="flex flex-wrap items-center justify-end gap-2"><Button size="sm" onClick={() => { setEditing(rule.id); setDraft({ tag: rule.tag, url: rule.url, notifyOnline: rule.notifyOnline, notifyOffline: rule.notifyOffline, enabled: rule.enabled }); }}>编辑</Button><Button size="sm" variant="ghost" className="text-danger" onClick={() => setDeleting(rule)}>删除</Button></div></article>)}</div>
    {!rules.length && <EmptyState title="暂无通知规则" detail="添加规则后，主机状态变化会发送到指定 URL。" />}
    {deleting && <ConfirmDialog title="删除通知规则" detail="该规则将停止发送后续通知。" onClose={() => setDeleting(undefined)} onConfirm={() => void remove()} />}
  </div>;
}

export const AdminIndex = () => <Navigate to="/admin/hosts" replace />;
