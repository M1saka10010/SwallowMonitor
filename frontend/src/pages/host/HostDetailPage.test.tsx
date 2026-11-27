import { cleanup, render, screen } from "@testing-library/react";
import type { ChartOptions } from "chart.js";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { HostDetailPage } from "./HostDetailPage";

const mocks = vi.hoisted(() => ({ host: vi.fn(), usage: vi.fn(), subscribe: () => () => {}, line: vi.fn((_props: { options: ChartOptions<"line"> }) => null) }));
vi.mock("../../api", () => ({ api: { host: mocks.host, usage: mocks.usage } }));
vi.mock("../../live/OverviewStreamProvider", () => ({ useOverviewStream: () => ({ subscribe: mocks.subscribe, generation: 0 }) }));
vi.mock("../../app/ThemeProvider", () => ({ useTheme: () => ({ theme: "light", revision: 0 }) }));
vi.mock("react-chartjs-2", () => ({ Line: mocks.line }));
vi.mock("../../app/AppContext", () => ({ useApp: () => ({ auth: { loggedIn: false } }) }));
afterEach(() => { cleanup(); mocks.line.mockClear(); });

it.each([["v0.1.0", "v0.1.0"], ["dev", "dev"], [undefined, "未知"], ["", "未知"], ["  ", "未知"]])("shows reported Agent version %s", async (agentVersion, expected) => {
  mocks.host.mockResolvedValue({ publicId: "host", nickname: "Test host", os: "linux", platform: "debian", platformVersion: "12", agentVersion });
  mocks.usage.mockResolvedValue([]);
  render(<MemoryRouter initialEntries={["/hosts/host"]}><Routes><Route path="/hosts/:publicId" element={<HostDetailPage />} /></Routes></MemoryRouter>);
  const label = await screen.findByText("Agent 版本");
  expect(label.nextElementSibling).toHaveTextContent(expected!);
});

it("formats chart tooltip titles as local date time", async () => {
  mocks.host.mockResolvedValue({ publicId: "host", nickname: "Test host" });
  mocks.usage.mockResolvedValue([]);
  render(<MemoryRouter initialEntries={["/hosts/host"]}><Routes><Route path="/hosts/:publicId" element={<HostDetailPage />} /></Routes></MemoryRouter>);
  await screen.findByRole("button", { name: "放大CPU图表" });

  const [{ options }] = mocks.line.mock.calls[0];
  const formatTitle = options.plugins?.tooltip?.callbacks?.title as (items: { parsed: { x: number } }[]) => string;
  const timestamp = 1700000000000;
  expect(formatTitle([{ parsed: { x: timestamp } }])).toBe(new Date(timestamp).toLocaleString());
  expect(formatTitle([])).toBe("");
});
