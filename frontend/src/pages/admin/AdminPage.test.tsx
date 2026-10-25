import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { api } from "../../api";
import type { Host, HostEnrollment } from "../../api/types";
import { copyText } from "../../utils/clipboard";
import { enrollmentCommand } from "../../utils/enrollment";
import { HostsPanel } from "./AdminPage";

vi.mock("react-router-dom", async (original) => ({ ...await original<typeof import("react-router-dom")>(), useOutletContext: () => ({ refresh: 0, invalidate: vi.fn() }) }));
vi.mock("../../utils/clipboard", () => ({ copyText: vi.fn().mockResolvedValue(undefined) }));
vi.mock("../../api", () => ({ api: { hosts: vi.fn(), tags: vi.fn(), createHost: vi.fn(), refreshEnrollment: vi.fn(), deleteHost: vi.fn() } }));
afterEach(() => { cleanup(); vi.resetAllMocks(); vi.unstubAllGlobals(); });
beforeEach(() => { vi.mocked(copyText).mockResolvedValue(undefined); });

const pending: Host = { publicId: "new", nickname: "Agent", tags: [], publicKey: "", online: false, hostId: "", hostname: "", os: "", platform: "", platformVersion: "", kernelArch: "", modelName: "", cores: 0, virtualizationRole: "", bootTime: 0, lastSeen: 0, createdAt: 0 };
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
  expect(await screen.findAllByText(/^printf %s/)).toHaveLength(2);
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
  expect(screen.queryByRole("button", { name: "复制迁移旧 Token Agent（v0.0.2）" })).not.toBeInTheDocument();
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
  expect(screen.queryByRole("button", { name: /升级|迁移/ })).not.toBeInTheDocument();
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

it("copies safely quoted pending migration and blocks copy at expiry even before timer rerender", async () => {
  vi.stubGlobal("location", new URL("https://monitor.example.com"));
  vi.mocked(api.hosts).mockResolvedValue([pending]); vi.mocked(api.tags).mockResolvedValue([]);
  const token = "a'b $(printf bad)";
  const expires = Math.floor(Date.now() / 1000) + 900;
  vi.mocked(api.refreshEnrollment).mockResolvedValue({ ...pending, enrollmentToken: token, enrollmentExpiresAt: expires });
  render(<HostsPanel />);
  fireEvent.click(await screen.findByRole("button", { name: "重新生成凭据" }));
  fireEvent.change(await screen.findByLabelText("目标 Agent release"), { target: { value: "v9.8.7" } });
  const button = await screen.findByRole("button", { name: "复制迁移旧 Token Agent（v0.0.2）" });
  fireEvent.click(button);
  expect(copyText).toHaveBeenCalledWith(enrollmentCommand(token, "wss://monitor.example.com/report", "v9.8.7", true));
  vi.mocked(copyText).mockClear();
  const clock = vi.spyOn(Date, "now").mockReturnValue(expires * 1000);
  fireEvent.click(button);
  expect(copyText).not.toHaveBeenCalled();
  expect(screen.queryByRole("button", { name: "复制新安装" })).not.toBeInTheDocument();
  clock.mockRestore();
});

it("requires an explicit valid release and removes commands when cleared or invalid", async () => {
  vi.stubGlobal("location", new URL("https://monitor.example.com"));
  vi.mocked(api.hosts).mockResolvedValue([pending]); vi.mocked(api.tags).mockResolvedValue([]);
  vi.mocked(api.refreshEnrollment).mockResolvedValue({ ...pending, enrollmentToken: "a".repeat(64), enrollmentExpiresAt: Math.floor(Date.now() / 1000) + 900 });
  render(<HostsPanel />);
  fireEvent.click(await screen.findByRole("button", { name: "重新生成凭据" }));
  const input = await screen.findByLabelText("目标 Agent release");
  expect(input).toHaveValue("");
  for (const value of ["", "latest", "v01.2.3", "v1.2.3;id", "v9.8.7", ""]) {
    fireEvent.change(input, { target: { value } });
    expect(screen.queryAllByText(/^printf %s/)).toHaveLength(value === "v9.8.7" ? 2 : 0);
    expect(screen.queryAllByRole("button", { name: /^复制/ })).toHaveLength(value === "v9.8.7" ? 2 : 0);
  }
});
