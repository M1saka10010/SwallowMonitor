import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { AppContextProvider } from "../../app/AppContext";
import { api } from "../../api";
import type { Host, HostEnrollment } from "../../api/types";
import { copyText } from "../../utils/clipboard";
import { enrollmentCommand } from "../../utils/enrollment";
import { AdminPage, HostsPanel, NotificationsPanel, TagsPanel } from "./AdminPage";

vi.mock("../../utils/clipboard", () => ({ copyText: vi.fn().mockResolvedValue(undefined) }));
vi.mock("../../api", () => ({ api: { agentReleases: vi.fn(), hosts: vi.fn(), tags: vi.fn(), createHost: vi.fn(), updateHost: vi.fn(), refreshEnrollment: vi.fn(), deleteHost: vi.fn(), createTag: vi.fn(), updateTag: vi.fn(), deleteTag: vi.fn(), notifications: vi.fn() } }));
afterEach(() => { cleanup(); vi.resetAllMocks(); vi.unstubAllGlobals(); });
const assets = ["bootstrap.sh", "checksums.txt", "install.sh", "upgrade.sh", "swallow-agent-linux-amd64", "swallow-agent-linux-arm64"];
const releaseData = { releases: [{ version: "v9.8.7", assets }], latestVersion: "v9.8.7", checkedAt: 1700000000, stale: false };
beforeEach(() => { vi.mocked(copyText).mockResolvedValue(undefined); vi.mocked(api.agentReleases).mockResolvedValue(releaseData); });

const pending: Host = { publicId: "new", nickname: "Agent", tags: [], publicKey: "", online: false, hostId: "", hostname: "", os: "", platform: "", platformVersion: "", kernelArch: "", modelName: "", cores: 0, virtualizationRole: "", bootTime: 0, lastSeen: 0, createdAt: 0 };
function renderAdmin(panel: "hosts" | "tags") {
  return render(<AppContextProvider value={{ auth: { loggedIn: true, authEnabled: true, user: "admin" }, settings: { siteName: "Monitor", siteDescription: "" }, setSettings: vi.fn() }}>
    <MemoryRouter initialEntries={[`/admin/${panel}`]}><Routes><Route path="/admin" element={<AdminPage />}>
      <Route path="hosts" element={<HostsPanel />} /><Route path="tags" element={<TagsPanel />} /><Route path="notifications" element={<NotificationsPanel />} />
    </Route></Routes></MemoryRouter>
  </AppContextProvider>);
}

it.each([
  ["hosts", "create"], ["hosts", "save"], ["hosts", "delete"],
  ["tags", "create"], ["tags", "save"], ["tags", "delete"],
] as const)("refreshes hosts and tags once after %s %s through AdminPage", async (panel, operation) => {
  const tag = { id: 1, name: "prod", createdAt: 0 };
  vi.mocked(api.hosts).mockResolvedValue([pending]); vi.mocked(api.tags).mockResolvedValue([tag]);
  vi.mocked(api.createHost).mockResolvedValue({ ...pending, enrollmentToken: "token", enrollmentExpiresAt: Date.now() / 1000 + 900 });
  vi.mocked(api.updateHost).mockResolvedValue({ status: "ok" }); vi.mocked(api.deleteHost).mockResolvedValue({ status: "ok" });
  vi.mocked(api.createTag).mockResolvedValue(tag); vi.mocked(api.updateTag).mockResolvedValue(tag); vi.mocked(api.deleteTag).mockResolvedValue({ status: "ok" });
  renderAdmin(panel);
  await screen.findByLabelText(panel === "hosts" ? "昵称" : "标签名称");
  expect(api.hosts).toHaveBeenCalledTimes(1); expect(api.tags).toHaveBeenCalledTimes(1);
  if (operation === "delete") fireEvent.click(screen.getByRole("button", { name: "删除" }));
  if (operation === "save") fireEvent.click(screen.getByRole("button", { name: "编辑" }));
  await act(async () => {
    if (operation === "delete") {
      fireEvent.click(screen.getByRole("button", { name: "确认删除" }));
    } else if (operation === "save") {
      fireEvent.click(screen.getByRole("button", { name: panel === "hosts" ? "保存" : "保存标签" }));
    } else {
      fireEvent.change(screen.getByLabelText(panel === "hosts" ? "昵称" : "标签名称"), { target: { value: "new item" } });
      fireEvent.click(screen.getByRole("button", { name: panel === "hosts" ? "添加主机" : "添加标签" }));
    }
  });
  const mutation = panel === "hosts" ? { create: api.createHost, save: api.updateHost, delete: api.deleteHost } : { create: api.createTag, save: api.updateTag, delete: api.deleteTag };
  expect(mutation[operation]).toHaveBeenCalledTimes(1);
  expect(api.hosts).toHaveBeenCalledTimes(2); expect(api.tags).toHaveBeenCalledTimes(2);
});

it.each(["success", "failure"])("keeps host creation busy until its refresh settles with %s", async (outcome) => {
  vi.mocked(api.hosts).mockResolvedValue([]); vi.mocked(api.tags).mockResolvedValue([]);
  vi.mocked(api.createHost).mockResolvedValue({ ...pending, enrollmentToken: "token", enrollmentExpiresAt: Date.now() / 1000 + 900 });
  renderAdmin("hosts");
  fireEvent.change(await screen.findByLabelText("昵称"), { target: { value: "Agent" } });
  const refresh = deferred<Host[]>(); vi.mocked(api.hosts).mockReturnValueOnce(refresh.promise);
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "添加主机" })); });
  expect(screen.getByRole("button", { name: "添加主机" })).toBeDisabled();
  await act(async () => { if (outcome === "success") refresh.resolve([pending]); else refresh.reject(new Error("refresh failed")); });
  expect(screen.getByRole("button", { name: "添加主机" })).toBeEnabled();
  if (outcome === "failure") expect(screen.getByText("refresh failed")).toBeInTheDocument();
  else expect(screen.getByText("Agent")).toBeInTheDocument();
  expect(api.hosts).toHaveBeenCalledTimes(2); expect(api.tags).toHaveBeenCalledTimes(2);
});

it("loads fresh hosts, tags and notification options when switching AdminPage panels", async () => {
  vi.mocked(api.hosts).mockResolvedValue([pending]); vi.mocked(api.tags).mockResolvedValue([]); vi.mocked(api.notifications).mockResolvedValue([]);
  renderAdmin("hosts"); await screen.findByText("Agent");
  const tag = { id: 1, name: "fresh-tag", createdAt: 0 };
  vi.mocked(api.tags).mockResolvedValue([tag]); vi.mocked(api.hosts).mockResolvedValue([{ ...pending, nickname: "Fresh host", tags: [tag.name] }]);
  fireEvent.click(screen.getByRole("link", { name: "标签管理" }));
  await screen.findByText("fresh-tag"); expect(screen.getByText(/1 台主机/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole("link", { name: "主机管理" }));
  await screen.findByText("Fresh host"); expect(screen.getByLabelText("fresh-tag")).toBeInTheDocument();
  expect(api.hosts).toHaveBeenCalledTimes(3); expect(api.tags).toHaveBeenCalledTimes(3);
  fireEvent.click(screen.getByRole("link", { name: "状态通知" }));
  await screen.findByRole("option", { name: "fresh-tag" });
  vi.mocked(api.tags).mockResolvedValue([{ ...tag, name: "renamed-tag" }]);
  fireEvent.click(screen.getByRole("link", { name: "标签管理" })); await screen.findByText("renamed-tag");
  fireEvent.click(screen.getByRole("link", { name: "状态通知" }));
  await screen.findByRole("option", { name: "renamed-tag" });
  expect(screen.queryByRole("option", { name: "fresh-tag" })).not.toBeInTheDocument();
  expect(api.notifications).toHaveBeenCalledTimes(2); expect(api.hosts).toHaveBeenCalledTimes(4); expect(api.tags).toHaveBeenCalledTimes(6);
});
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

it.each(["success", "failure"])("ignores an older host refresh %s after a newer response", async (outcome) => {
  vi.mocked(api.hosts).mockResolvedValue([pending]); vi.mocked(api.tags).mockResolvedValue([]);
  render(<HostsPanel />); await screen.findByText("Agent");
  const older = deferred<Host[]>(); const newer = deferred<Host[]>();
  vi.mocked(api.hosts).mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise);
  fireEvent.click(screen.getByRole("button", { name: "刷新主机版本" }));
  fireEvent.click(screen.getByRole("button", { name: "刷新主机版本" }));
  await act(async () => { newer.resolve([{ ...pending, agentVersion: "v9.8.7" }]); });
  expect(screen.getByText(/已是最新正式版/)).toBeInTheDocument();
  await act(async () => { if (outcome === "success") older.resolve([{ ...pending, agentVersion: "v1.0.0" }]); else older.reject(new Error("obsolete failure")); });
  expect(screen.getByText(/Agent v9.8.7/)).toBeInTheDocument();
  expect(screen.getByText(/已是最新正式版/)).toBeInTheDocument();
  expect(screen.queryByText("obsolete failure")).not.toBeInTheDocument();
});

it("waits ten minutes after release completion, retries failures, and cleans up pending work", async () => {
  vi.useFakeTimers();
  try {
    vi.mocked(api.hosts).mockResolvedValue([pending]); vi.mocked(api.tags).mockResolvedValue([]);
    const first = deferred<typeof releaseData>(); const last = deferred<typeof releaseData>();
    vi.mocked(api.agentReleases).mockReturnValueOnce(first.promise).mockRejectedValueOnce(new Error("offline")).mockReturnValueOnce(last.promise);
    const view = render(<HostsPanel />);
    await act(async () => { await vi.advanceTimersByTimeAsync(30000); });
    await act(async () => { first.resolve(releaseData); });
    await act(async () => { await vi.advanceTimersByTimeAsync(599999); });
    expect(api.agentReleases).toHaveBeenCalledTimes(1);
    await act(async () => { await vi.advanceTimersByTimeAsync(1); });
    expect(api.agentReleases).toHaveBeenCalledTimes(2);
    expect(screen.getByText(/发布列表加载失败：offline/)).toBeInTheDocument();
    await act(async () => { await vi.advanceTimersByTimeAsync(600000); });
    expect(api.agentReleases).toHaveBeenCalledTimes(3);
    const signal = vi.mocked(api.agentReleases).mock.calls[2][0];
    const host = deferred<Host[]>();
    vi.mocked(api.hosts).mockReturnValueOnce(host.promise);
    fireEvent.click(screen.getByRole("button", { name: "刷新主机版本" }));
    view.unmount();
    expect(signal?.aborted).toBe(true);
    await act(async () => { last.resolve(releaseData); host.resolve([pending]); });
    expect(vi.getTimerCount()).toBe(0);
    await vi.advanceTimersByTimeAsync(600000);
    expect(api.agentReleases).toHaveBeenCalledTimes(3);
  } finally { vi.useRealTimers(); }
});

it.each(["", "v9.8.7"])("preserves explicit target %s through retry and polling even if removed", async (selection) => {
  vi.useFakeTimers();
  try {
    vi.mocked(api.hosts).mockResolvedValue([{ ...pending, publicKey: "key", os: "linux", kernelArch: "x86_64", agentVersion: "v1.0.0" }]); vi.mocked(api.tags).mockResolvedValue([]);
    const view = render(<HostsPanel />);
    await act(async () => { await vi.advanceTimersByTimeAsync(0); });
    fireEvent.change(screen.getByLabelText("目标 Agent release"), { target: { value: selection } });
    vi.mocked(api.agentReleases).mockResolvedValue({ ...releaseData, releases: [{ version: "v10.0.0", assets }], latestVersion: "v10.0.0" });
    const signal = vi.mocked(api.agentReleases).mock.calls[0][0];
    fireEvent.click(screen.getByRole("button", { name: "重试发布列表" }));
    await act(async () => { await vi.advanceTimersByTimeAsync(0); });
    expect(signal?.aborted).toBe(true);
    expect(screen.getByLabelText("目标 Agent release")).toHaveValue(selection);
    expect(screen.queryByRole("button", { name: "升级 Agent" })).not.toBeInTheDocument();
    if (selection) expect(screen.getByRole("option", { name: /已不可用，请重新选择/ })).toBeDisabled();
    await act(async () => { await vi.advanceTimersByTimeAsync(600000); });
    expect(api.agentReleases).toHaveBeenCalledTimes(3);
    expect(screen.getByLabelText("目标 Agent release")).toHaveValue(selection);
    view.unmount(); expect(vi.getTimerCount()).toBe(0);
  } finally { vi.useRealTimers(); }
});
it("creates and rotates credentials without keys, expires and forgets plaintext on remount", async () => {
  vi.stubGlobal("location", new URL("https://monitor.example.com"));
  vi.mocked(api.hosts).mockResolvedValue([pending]);
  vi.mocked(api.tags).mockResolvedValue([]);
  const expires = Math.floor(Date.now() / 1000) + 900;
  vi.mocked(api.createHost).mockResolvedValue({ ...pending, enrollmentToken: "a".repeat(64), enrollmentExpiresAt: expires });
  vi.mocked(api.refreshEnrollment).mockResolvedValue({ ...pending, enrollmentToken: "b".repeat(64), enrollmentExpiresAt: expires });
  const view = render(<HostsPanel />);
  fireEvent.change(await screen.findByLabelText("昵称"), { target: { value: "Agent" } });
  fireEvent.click(screen.getByRole("button", { name: "添加主机" }));
  fireEvent.change(await screen.findByLabelText("目标 Agent release"), { target: { value: "v9.8.7" } });
  expect(await screen.findAllByText(/^printf %s/)).toHaveLength(1);
  for (const command of screen.getAllByText(/^printf %s/)) expect(command).toHaveTextContent("a".repeat(64));
  expect(api.createHost).toHaveBeenCalledWith({ nickname: "Agent", tags: [] });
  expect(screen.queryByLabelText(/公钥/)).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "重新生成凭据" }));
  await waitFor(() => expect(screen.getAllByText(/^printf %s/).every((node) => node.textContent?.includes("b".repeat(64)))).toBe(true));
  expect(api.refreshEnrollment).toHaveBeenCalledWith("new");
  for (const command of screen.getAllByText(/^printf %s/)) expect(command).not.toHaveTextContent("a".repeat(64));
  const clock = vi.spyOn(Date, "now").mockReturnValue((expires + 1) * 1000);
  await screen.findByText(/注册凭据已过期/, {}, { timeout: 2500 });
  expect(screen.queryByRole("button", { name: "复制新安装" })).not.toBeInTheDocument();
  clock.mockRestore(); view.unmount(); render(<HostsPanel />);
  await screen.findByText(/待注册：/);
  expect(screen.queryByText(/一次性注册凭据仅本次显示/)).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "删除" }));
  expect(screen.getByText(/将删除“Agent”及其全部历史数据/)).toBeInTheDocument();
});
it("offers no enrollment or routine update commands for bound hosts", async () => {
  vi.mocked(api.hosts).mockResolvedValue([{ ...pending, publicKey: "A".repeat(43) + "=" }]);
  vi.mocked(api.tags).mockResolvedValue([]);
  render(<HostsPanel />);
  await screen.findByText("Agent");
  expect(screen.queryByRole("button", { name: "重新生成凭据" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /升级/ })).not.toBeInTheDocument();
  expect(screen.queryByText(/^sudo |^printf %s/)).not.toBeInTheDocument();
});
it("clears stale plaintext even when renewal fails", async () => {
  vi.mocked(api.hosts).mockResolvedValue([pending]); vi.mocked(api.tags).mockResolvedValue([]);
  vi.mocked(api.refreshEnrollment).mockRejectedValue(new Error("already bound"));
  render(<HostsPanel />);
  fireEvent.click(await screen.findByRole("button", { name: "重新生成凭据" }));
  expect(await screen.findByText("already bound")).toBeInTheDocument();
  expect(screen.queryByText(/^printf %s/)).not.toBeInTheDocument();
});

it.each([
  ['https://monitor.example.com', 'wss://monitor.example.com/report'],
  ['http://localhost:8080', 'ws://localhost:8080/report'],
  ['http://127.0.0.2:8080', 'ws://127.0.0.2:8080/report'],
  ['http://[::1]:8080', 'ws://[::1]:8080/report'],
  ['http://monitor.example.com', undefined],
  ['http://localhost.example.com', undefined],
])('shows safe connection guidance on %s', async (origin, expected) => {
  vi.stubGlobal('location', new URL(origin));
  vi.mocked(api.hosts).mockResolvedValue([pending] as unknown as Host[]);
  vi.mocked(api.tags).mockResolvedValue([]);
  vi.mocked(api.refreshEnrollment).mockResolvedValue({ ...pending, enrollmentToken: 'a'.repeat(64), enrollmentExpiresAt: Math.floor(Date.now() / 1000) + 900 } as HostEnrollment);
  render(<HostsPanel />);
  fireEvent.click(await screen.findByRole('button', { name: '重新生成凭据' }));
  await screen.findByText(/一次性注册凭据仅本次显示/);
  expect(screen.getByText(/生产环境必须配置实际 WSS/)).toHaveTextContent('devMode: true');
  fireEvent.change(screen.getByLabelText("目标 Agent release"), { target: { value: "v9.8.7" } });
  if (expected) {
    for (const command of screen.getAllByText(/^printf %s/)) expect(command).toHaveTextContent(expected);
  } else {
    expect(screen.queryByText('Report URL')).not.toBeInTheDocument();
    expect(screen.getByText(/无法生成安全的 Report URL/)).toBeInTheDocument();
    expect(screen.queryByText(/^ws:\/\//)).not.toBeInTheDocument();
  }
});

it("copies safely quoted pending install command and blocks copy at expiry even before timer rerender", async () => {
  vi.stubGlobal("location", new URL("https://monitor.example.com"));
  vi.mocked(api.hosts).mockResolvedValue([pending]); vi.mocked(api.tags).mockResolvedValue([]);
  const token = "a'b $(printf bad)";
  const expires = Math.floor(Date.now() / 1000) + 900;
  vi.mocked(api.refreshEnrollment).mockResolvedValue({ ...pending, enrollmentToken: token, enrollmentExpiresAt: expires });
  render(<HostsPanel />);
  fireEvent.click(await screen.findByRole("button", { name: "重新生成凭据" }));
  fireEvent.change(await screen.findByLabelText("目标 Agent release"), { target: { value: "v9.8.7" } });
  const button = await screen.findByRole("button", { name: "复制新安装" });
  fireEvent.click(button);
  expect(copyText).toHaveBeenCalledWith(enrollmentCommand(token, "wss://monitor.example.com/report", "v9.8.7"));
  vi.mocked(copyText).mockClear();
  const clock = vi.spyOn(Date, "now").mockReturnValue(expires * 1000);
  fireEvent.click(button);
  expect(copyText).not.toHaveBeenCalled();
  expect(screen.queryByRole("button", { name: "复制新安装" })).not.toBeInTheDocument();
  clock.mockRestore();
});

it("defaults to the newest installable release and only accepts listed versions", async () => {
  vi.stubGlobal("location", new URL("https://monitor.example.com"));
  vi.mocked(api.hosts).mockResolvedValue([pending]); vi.mocked(api.tags).mockResolvedValue([]);
  vi.mocked(api.refreshEnrollment).mockResolvedValue({ ...pending, enrollmentToken: "a".repeat(64), enrollmentExpiresAt: Math.floor(Date.now() / 1000) + 900 });
  render(<HostsPanel />);
  fireEvent.click(await screen.findByRole("button", { name: "重新生成凭据" }));
  const input = await screen.findByLabelText("目标 Agent release");
  expect(input).toHaveValue("v9.8.7");
  for (const value of ["", "latest", "v01.2.3", "v1.2.3;id", "v9.8.7", ""]) {
    fireEvent.change(input, { target: { value } });
    expect(screen.queryAllByText(/^printf %s/)).toHaveLength(value === "v9.8.7" ? 1 : 0);
    expect(screen.queryAllByRole("button", { name: /^复制/ })).toHaveLength(value === "v9.8.7" ? 1 : 0);
  }
});

it("expands and copies upgrades using actual kernelArch, gates proxy and disables invalid input", async () => {
  vi.mocked(api.hosts).mockResolvedValue([{ ...pending, publicKey: "key", os: "linux", kernelArch: "aarch64", agentVersion: "v1.0.0" }]); vi.mocked(api.tags).mockResolvedValue([]);
  render(<HostsPanel />);
  fireEvent.click(await screen.findByRole("button", { name: "升级 Agent" }));
  fireEvent.click(screen.getByRole("button", { name: "复制升级命令" }));
  expect(copyText).toHaveBeenLastCalledWith(expect.stringContaining("--mode upgrade"));
  expect(vi.mocked(copyText).mock.calls[0][0]).not.toContain("--enrollment-token");
  const proxy = screen.getByLabelText("GitHub HTTPS 代理前缀（可选）");
  fireEvent.change(proxy, { target: { value: "https://proxy.example.com" } });
  fireEvent.click(screen.getByRole("button", { name: "复制升级命令" }));
  expect(copyText).toHaveBeenLastCalledWith(expect.stringContaining("https://proxy.example.com/https://github.com/"));
  fireEvent.change(proxy, { target: { value: "https://user:pass@proxy.example.com" } });
  expect(screen.getByRole("button", { name: "升级 Agent" })).toBeDisabled();
  expect(screen.queryByRole("button", { name: "复制升级命令" })).not.toBeInTheDocument();
});

it.each(["dev", "unknown", "", "v01.0.0"])("does not guess upgrade state for %s", async (agentVersion) => {
  vi.mocked(api.hosts).mockResolvedValue([{ ...pending, publicKey: "key", os: "linux", kernelArch: "x86_64", agentVersion }]); vi.mocked(api.tags).mockResolvedValue([]);
  render(<HostsPanel />); await screen.findByText(/版本未知，无法判断/);
  expect(screen.queryByRole("button", { name: "升级 Agent" })).not.toBeInTheDocument();
});

it("reports failures, retries, and never claims stale data is latest", async () => {
  vi.mocked(api.hosts).mockResolvedValue([{ ...pending, agentVersion: "v9.8.7" }]); vi.mocked(api.tags).mockResolvedValue([]);
  vi.mocked(api.agentReleases).mockRejectedValueOnce(new Error("offline")).mockResolvedValueOnce({ ...releaseData, stale: true });
  render(<HostsPanel />); await screen.findByText(/发布列表加载失败：offline/);
  expect(screen.getByText(/发布状态未知/)).toBeInTheDocument();
  expect(screen.queryByText(/已是最新正式版/)).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "重试发布列表" }));
  await screen.findByText(/发布列表已过期/); expect(screen.queryByText(/已是最新正式版/)).not.toBeInTheDocument();
});

it("gates installation by release assets and disables proxy for older releases", async () => {
  vi.stubGlobal("location", new URL("https://monitor.example.com"));
  vi.mocked(api.hosts).mockResolvedValue([pending]); vi.mocked(api.tags).mockResolvedValue([]);
  vi.mocked(api.agentReleases).mockResolvedValue({ ...releaseData, releases: [{ version: "v10.0.0", assets: [] }, { version: "v9.8.7", assets: assets.filter((asset) => asset !== "upgrade.sh") }] });
  vi.mocked(api.refreshEnrollment).mockResolvedValue({ ...pending, enrollmentToken: "token", enrollmentExpiresAt: Date.now() / 1000 + 900 });
  render(<HostsPanel />); fireEvent.click(await screen.findByRole("button", { name: "重新生成凭据" }));
  expect(await screen.findByRole("button", { name: "复制新安装" })).toBeInTheDocument();
  expect(screen.getByLabelText("目标 Agent release")).toHaveValue("v9.8.7");
  expect(screen.getByLabelText("GitHub HTTPS 代理前缀（可选）")).toBeDisabled();
  expect(screen.getByText(/^printf %s/)).not.toHaveTextContent("--github-proxy");
});

it("refreshes hosts and releases every ten minutes and follows latest until explicitly selected", async () => {
  vi.useFakeTimers();
  try {
    vi.mocked(api.hosts).mockResolvedValue([{ ...pending, agentVersion: "v9.8.7" }]); vi.mocked(api.tags).mockResolvedValue([]);
    const view = render(<HostsPanel />);
    await act(async () => { await vi.advanceTimersByTimeAsync(0); });
    expect(screen.getByLabelText("目标 Agent release")).toHaveValue("v9.8.7");
    const updated = { ...releaseData, latestVersion: "v10.0.0", releases: [{ version:"v10.0.0",assets }, ...releaseData.releases] };
    vi.mocked(api.agentReleases).mockResolvedValue(updated);
    await act(async () => { await vi.advanceTimersByTimeAsync(600000); });
    expect(api.hosts).toHaveBeenCalledTimes(2); expect(api.agentReleases).toHaveBeenCalledTimes(2);
    expect(screen.getByLabelText("目标 Agent release")).toHaveValue("v10.0.0");
    fireEvent.change(screen.getByLabelText("目标 Agent release"), { target: { value: "v9.8.7" } });
    await act(async () => { await vi.advanceTimersByTimeAsync(600000); });
    expect(screen.getByLabelText("目标 Agent release")).toHaveValue("v9.8.7");
    view.unmount();
  } finally { vi.useRealTimers(); }
});

it("recovers host refresh failures without leaving version status permanently unknown", async () => {
  vi.mocked(api.hosts).mockResolvedValue([{ ...pending, agentVersion: "v9.8.7" }]); vi.mocked(api.tags).mockResolvedValue([]);
  render(<HostsPanel />); await screen.findByText(/已是最新正式版/);
  vi.mocked(api.hosts).mockRejectedValueOnce(new Error("host refresh failed"));
  fireEvent.click(screen.getByRole("button", { name: "刷新主机版本" })); await screen.findByText("host refresh failed");
  expect(screen.queryByText(/已是最新正式版/)).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "刷新主机版本" })); await screen.findByText(/已是最新正式版/);
  expect(screen.queryByText("host refresh failed")).not.toBeInTheDocument();
});
