import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { Host, MaintenanceTask } from "../../api/types";
import { HostMaintenancePanel, MaintenancePage } from "./Maintenance";

const mocks = vi.hoisted(() => ({ loggedIn: true, maintenance: vi.fn(), hostMaintenance: vi.fn(), agentReleases: vi.fn(), createMaintenance: vi.fn(), maintenanceDetail: vi.fn() }));
vi.mock("../../api", () => ({ api: mocks }));
vi.mock("../../app/AppContext", () => ({ useApp: () => ({ auth: { loggedIn: mocks.loggedIn } }) }));
const host = { publicId: "host-a", nickname: "A", online: true, os: "linux", kernelArch: "amd64", agentVersion: "v1.0.0" } as Host;
const task = { id: "task-a", publicId: "host-a", kind: "system_update", status: "queued", targetVersion: "", createdBy: "admin", createdAt: 1700000000, updatedAt: 1700000000, lastSeq: 0 } as MaintenanceTask;
beforeEach(() => {
  vi.resetAllMocks(); mocks.loggedIn = true;
  mocks.hostMaintenance.mockResolvedValue({ supported: true, online: true, tasks: [] });
  mocks.agentReleases.mockResolvedValue({ stale: false, releases: ["v0.9.0", "v1.0.0", "v1.1.0-rc.1", "v1.1.0"].map(version => ({ version, assets: ["bootstrap.sh", "checksums.txt", "upgrade.sh", "swallow-agent-linux-amd64"] })) });
  mocks.maintenance.mockResolvedValue({ tasks: [task] });
  mocks.maintenanceDetail.mockResolvedValue({ task, events: [], nextAfter: 0, hasMore: false });
  mocks.createMaintenance.mockResolvedValue(task);
});
afterEach(() => { cleanup(); vi.useRealTimers(); });

it("does not fetch or show controls without login", () => {
  mocks.loggedIn = false;
  render(<><HostMaintenancePanel host={host} /><MaintenancePage /></>);
  expect(mocks.hostMaintenance).not.toHaveBeenCalled(); expect(mocks.maintenance).not.toHaveBeenCalled();
  expect(screen.queryByRole("button")).toBeNull();
});

it("offers only supported newer stable releases and confirms before creation", async () => {
  render(<HostMaintenancePanel host={host} />);
  await waitFor(() => expect(screen.getByRole("button", { name: "升级 Agent" })).toBeEnabled());
  expect(screen.getAllByRole("option").map(option => option.textContent)).toEqual(["v1.1.0"]);
  fireEvent.click(screen.getByRole("button", { name: "升级 Agent" }));
  expect(screen.getByRole("dialog")).toHaveTextContent("不会自动重启主机");
  expect(screen.getByRole("dialog")).toHaveTextContent("dist-upgrade");
  expect(mocks.createMaintenance).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "确认执行" }));
  await waitFor(() => expect(mocks.createMaintenance).toHaveBeenCalledWith("host-a", { kind: "agent_upgrade", targetVersion: "v1.1.0" }, expect.any(AbortSignal)));
  expect(await screen.findByRole("region", { name: "任务详情" })).toBeInTheDocument();
});

it.each(["queued", "delivered", "accepted", "running", "verifying", "unknown"])("blocks %s tasks", async status => {
  mocks.hostMaintenance.mockResolvedValue({ supported: true, online: true, tasks: [{ ...task, status }] });
  render(<HostMaintenancePanel host={host} />);
  await screen.findByText("查看任务 task-a");
  expect(screen.getByRole("button", { name: "更新系统软件包" })).toBeDisabled();
});

it.each([{ supported: false, online: true }, { supported: true, online: false }])("blocks unsupported/offline hosts %j", async state => {
  mocks.hostMaintenance.mockResolvedValue({ ...state, tasks: [] });
  render(<HostMaintenancePanel host={host} />);
  await screen.findByRole("option", { name: "v1.1.0" });
  expect(screen.getByText(state.supported ? "主机离线，暂不可创建维护任务。" : "此主机不支持远程维护。")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "更新系统软件包" })).toBeDisabled();
});

it("locks uncertain requests without retrying", async () => {
  mocks.createMaintenance.mockRejectedValue(new Error("network"));
  render(<HostMaintenancePanel host={host} />);
  await waitFor(() => expect(screen.getByRole("button", { name: "更新系统软件包" })).toBeEnabled());
  fireEvent.click(screen.getByRole("button", { name: "更新系统软件包" }));
  fireEvent.click(screen.getByRole("button", { name: "确认执行" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("请求结果不确定");
  expect(screen.getByRole("button", { name: "更新系统软件包" })).toBeDisabled();
  expect(mocks.createMaintenance).toHaveBeenCalledTimes(1);
});

it("uses opaque numeric pagination and drains event cursors with escaped logs", async () => {
  mocks.maintenance.mockResolvedValueOnce({ tasks: [task], nextBefore: 42 }).mockResolvedValue({ tasks: [task] });
  mocks.maintenanceDetail.mockResolvedValueOnce({ task, events: [{ seq: 1, status: "running", createdAt: 1700000000, message: "<script>bad()</script>" }], nextAfter: 7, hasMore: true }).mockResolvedValue({ task, events: [{ seq: 8, status: "succeeded", createdAt: 1700000001, message: "done" }], nextAfter: 8, hasMore: false });
  const { container } = render(<MaintenancePage />);
  fireEvent.click(await screen.findByRole("button", { name: "下一页" }));
  await waitFor(() => expect(mocks.maintenance).toHaveBeenLastCalledWith(undefined, 42, expect.any(AbortSignal)));
  fireEvent.click(await screen.findByRole("button", { name: "查看任务 task-a" }));
  await waitFor(() => expect(mocks.maintenanceDetail).toHaveBeenCalledWith("task-a", 7, expect.any(AbortSignal)));
  expect(container.querySelector("pre")).toHaveTextContent("<script>bad()</script>");
  expect(container.querySelector("script")).toBeNull();
  expect(container.querySelector("time")).toHaveTextContent(new Date(1700000000 * 1000).toLocaleString());
});

it("does not overlap polling and aborts pending reads on unmount", async () => {
  vi.useFakeTimers();
  mocks.maintenance.mockReturnValue(new Promise(() => {}));
  const { unmount } = render(<MaintenancePage />);
  await act(async () => { await vi.advanceTimersByTimeAsync(20000); });
  expect(mocks.maintenance).toHaveBeenCalledTimes(1);
  const signal = mocks.maintenance.mock.calls[0][2];
  unmount(); expect(signal.aborted).toBe(true);
});

it("resets host controls and ignores stale responses on host change", async () => {
  let resolve!: (value: unknown) => void;
  mocks.hostMaintenance.mockReturnValueOnce(new Promise(done => { resolve = done; })).mockResolvedValue({ supported: false, online: false, tasks: [] });
  const { rerender } = render(<HostMaintenancePanel host={host} />);
  const signal = mocks.hostMaintenance.mock.calls[0][1];
  rerender(<HostMaintenancePanel host={{ ...host, publicId: "host-b" }} />);
  await act(async () => resolve({ supported: true, online: true, tasks: [task] }));
  expect(signal.aborted).toBe(true);
  expect(screen.queryByText("查看任务 task-a")).toBeNull();
  expect(screen.getByRole("button", { name: "更新系统软件包" })).toBeDisabled();
});

it("preserves history on polling failure and enables controls only after recovery", async () => {
  vi.useFakeTimers();
  const ready = { supported: true, online: true, tasks: [{ ...task, status: "succeeded" }] };
  mocks.hostMaintenance.mockResolvedValueOnce(ready).mockRejectedValueOnce(new Error("network")).mockResolvedValue(ready);
  render(<HostMaintenancePanel host={host} />);
  await act(async () => {});
  expect(screen.getByRole("button", { name: "更新系统软件包" })).toBeEnabled();
  await act(async () => { await vi.advanceTimersByTimeAsync(5000); });
  expect(screen.getByRole("button", { name: "查看任务 task-a" })).toBeInTheDocument();
  expect(screen.getByRole("alert")).toHaveTextContent("主机维护状态未确认");
  expect(screen.getByRole("button", { name: "更新系统软件包" })).toBeDisabled();
  await act(async () => { await vi.advanceTimersByTimeAsync(5000); });
  expect(screen.getByRole("button", { name: "更新系统软件包" })).toBeEnabled();
});

it("explains stale release data and release errors", async () => {
  mocks.agentReleases.mockResolvedValue({ stale: true, error: "upstream unavailable", releases: [] });
  render(<HostMaintenancePanel host={host} />);
  expect(await screen.findByText("发布信息已过期，暂不可升级 Agent。")).toBeInTheDocument();
  expect(screen.getByText("发布信息异常：upstream unavailable")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "升级 Agent" })).toBeDisabled();
});

it("localizes delivered status and task kinds without claiming acknowledgment", async () => {
  mocks.maintenance.mockResolvedValue({ tasks: [{ ...task, status: "delivered" }] });
  render(<MaintenancePage />);
  expect(await screen.findByText("已发起投递，等待确认")).toBeInTheDocument();
  expect(screen.getByText(/更新系统软件包/)).toBeInTheDocument();
});
