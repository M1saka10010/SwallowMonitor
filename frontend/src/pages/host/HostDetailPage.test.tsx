import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { HostDetailPage } from "./HostDetailPage";

const mocks = vi.hoisted(() => ({ host: vi.fn(), usage: vi.fn(), subscribe: () => () => {} }));
vi.mock("../../api", () => ({ api: { host: mocks.host, usage: mocks.usage } }));
vi.mock("../../live/OverviewStreamProvider", () => ({ useOverviewStream: () => ({ subscribe: mocks.subscribe, generation: 0 }) }));
vi.mock("../../app/ThemeProvider", () => ({ useTheme: () => ({ theme: "light", revision: 0 }) }));
vi.mock("react-chartjs-2", () => ({ Line: () => null }));
afterEach(cleanup);

it.each([["v0.1.0", "v0.1.0"], ["dev", "dev"], [undefined, "未知"], ["", "未知"], ["  ", "未知"]])("shows reported Agent version %s", async (agentVersion, expected) => {
  mocks.host.mockResolvedValue({ publicId: "host", nickname: "Test host", os: "linux", platform: "debian", platformVersion: "12", agentVersion });
  mocks.usage.mockResolvedValue([]);
  render(<MemoryRouter initialEntries={["/hosts/host"]}><Routes><Route path="/hosts/:publicId" element={<HostDetailPage />} /></Routes></MemoryRouter>);
  const label = await screen.findByText("Agent 版本");
  expect(label.nextElementSibling).toHaveTextContent(expected!);
});
