import { useCallback, useEffect, useState, type FormEvent } from "react";
import { NavLink, Navigate, Outlet, useOutletContext } from "react-router-dom";
import { api } from "../../api";
import type { Host, NotificationRule, Tag } from "../../api/types";
import { Button, ConfirmDialog, EmptyState, PageState, StatusBadge, fieldClass } from "../../components/ui";
import { useApp } from "../../app/AppContext";
import { copyText, installationCommand } from "../../utils/clipboard";
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
    <div className="mb-6 border-b border-line pb-5"><p className="font-mono text-[11px] uppercase tracking-[0.16em] text-faint">Administration</p><h1 className="mt-2 font-serif text-[28px] font-semibold leading-tight tracking-tight">后台管理</h1></div>
    <div className="grid gap-6 lg:grid-cols-[180px_minmax(0,1fr)]">
      <nav className="flex gap-4 overflow-x-auto border-b border-line lg:block lg:space-y-1 lg:border-b-0 lg:border-r lg:pr-5" aria-label="后台管理">{panels.map(([path, label]) => <NavLink key={path} to={`/admin/${path}`} className={navLinkClass}>{label}</NavLink>)}</nav>
      <div className="min-w-0"><Outlet context={{ refresh, invalidate: () => setRefresh((value) => value + 1) }} /></div>
    </div>
  </section>;
}

function FieldMessage({ value, success = false }: { value: string; success?: boolean }) { return value ? <p aria-live="polite" className={`text-sm ${success ? "text-success" : "text-danger"}`}>{value}</p> : null; }
function CheckTags({ tags, value, onChange }: { tags: Tag[]; value: string[]; onChange: (value: string[]) => void }) { return <div className="flex flex-wrap gap-3">{tags.map((tag) => <label key={tag.id} className="flex min-h-11 items-center gap-2 text-sm"><input type="checkbox" checked={value.includes(tag.name)} onChange={(event) => onChange(event.target.checked ? [...value, tag.name] : value.filter((item) => item !== tag.name))} />{tag.name}</label>)}</div>; }

export function HostsPanel() {
  const { refresh, invalidate } = useAdmin(); const [hosts, setHosts] = useState<Host[]>(); const [tags, setTags] = useState<Tag[]>([]); const [error, setError] = useState(""); const [copyMessage, setCopyMessage] = useState(""); const [nickname, setNickname] = useState(""); const [token, setToken] = useState(""); const [selected, setSelected] = useState<string[]>([]); const [created, setCreated] = useState<Host>(); const [editing, setEditing] = useState<Host>(); const [deleting, setDeleting] = useState<Host>();
  const load = useCallback(() => Promise.all([api.hosts(), api.tags()]).then(([nextHosts, nextTags]) => { setHosts(nextHosts); setTags(nextTags); }).catch((reason) => setError(errorMessage(reason))), []);
  useEffect(() => { void load(); }, [load, refresh]);
  const create = async (event: FormEvent) => { event.preventDefault(); setError(""); try { const host = await api.createHost({ nickname, token, tags: selected }); setCreated(host); setNickname(""); setToken(""); setSelected([]); await load(); invalidate(); } catch (reason) { setError(errorMessage(reason)); } };
  const save = async () => { if (!editing) return; try { await api.updateHost(editing.publicId, { nickname: editing.nickname, tags: editing.tags }); setEditing(undefined); await load(); invalidate(); } catch (reason) { setError(errorMessage(reason)); } };
  const remove = async () => { if (!deleting) return; try { await api.deleteHost(deleting.publicId); setDeleting(undefined); await load(); invalidate(); } catch (reason) { setError(errorMessage(reason)); } };
  if (!hosts && error) return <PageState label="ERROR" tone="danger" message={`主机管理加载失败：${error}`} action={<Button onClick={() => { setError(""); void load(); }}>重新加载</Button>} />;
  if (!hosts) return <PageState message="正在加载主机管理…" />;
  return <div>
    <h2 className="text-lg font-semibold">主机管理</h2>
    <form onSubmit={create} className={`mt-4 ${zoneClass}`}><div className="grid gap-3 md:grid-cols-2"><label className="text-sm text-muted">昵称<input className={`${fieldClass} mt-1`} value={nickname} onChange={(event) => setNickname(event.target.value)} required /></label><label className="text-sm text-muted">Token（留空自动生成）<input className={`${fieldClass} mt-1 font-mono`} value={token} onChange={(event) => setToken(event.target.value)} /></label></div><div><span className="text-sm text-muted">标签</span><CheckTags tags={tags} value={selected} onChange={setSelected} /></div><div><Button variant="primary" type="submit">添加主机</Button></div><FieldMessage value={error} /></form>
    {created?.token && <div className="mt-4 rounded-[10px] border border-dashed border-line-strong bg-surface p-4"><p className="text-sm font-medium">主机已添加。保存 Token 和安装命令。</p>{[["Token", created.token], ["安装命令", installationCommand(created.token)]].map(([label, value]) => <div key={label} className="mt-3 grid items-center gap-2 sm:grid-cols-[80px_minmax(0,1fr)_auto]"><span className="text-xs text-faint">{label}</span><code className="overflow-hidden break-all rounded-md bg-surface-muted p-2 font-mono text-xs">{value}</code><Button size="sm" onClick={() => void copyText(value).then(() => setCopyMessage(`${label}已复制`)).catch((reason) => setError(errorMessage(reason)))}>复制</Button></div>)}<FieldMessage value={copyMessage} success /></div>}
    <div className="mt-8"><h3 className="mb-2 text-sm font-semibold">主机列表</h3>{!hosts.length ? <EmptyState title="暂无主机" detail="使用上方表单添加第一台主机。" /> : <div className="divide-y divide-line overflow-hidden rounded-[10px] border border-line">{hosts.map((host) => { const current = editing?.publicId === host.publicId ? editing : undefined; return <article key={host.publicId} className="grid gap-3 bg-surface px-4 py-3 shadow-[inset_0_1px_0_var(--edge-light)] lg:grid-cols-[minmax(240px,1.3fr)_minmax(220px,1fr)_auto] lg:items-center"><div className="min-w-0">{current ? <input className={fieldClass} value={current.nickname} onChange={(event) => setEditing({ ...current, nickname: event.target.value })} /> : <div className="flex items-center gap-2.5"><p className="truncate text-sm font-medium">{host.nickname}</p><StatusBadge online={host.online} /></div>}<p className="mt-1 break-all font-mono text-xs text-faint">{host.publicId}</p></div><div>{current ? <CheckTags tags={tags} value={current.tags} onChange={(value) => setEditing({ ...current, tags: value })} /> : host.tags.length ? <div className="flex flex-wrap gap-1">{host.tags.map((tag) => <span key={tag} className="rounded-full border border-line px-2.5 py-1 text-xs text-muted">{tag}</span>)}</div> : <span className="text-xs text-faint">未分组</span>}</div><div className="flex flex-wrap items-center justify-end gap-2">{current ? <><Button size="sm" variant="primary" onClick={() => void save()}>保存</Button><Button size="sm" onClick={() => setEditing(undefined)}>取消</Button></> : <><Button size="sm" onClick={() => setEditing({ ...host, tags: [...host.tags] })}>编辑</Button>{host.token && <Button size="sm" onClick={() => setCreated(host)}>安装命令</Button>}<Button size="sm" variant="ghost" className="text-danger" onClick={() => setDeleting(host)}>删除</Button></>}</div></article>; })}</div>}</div>
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
