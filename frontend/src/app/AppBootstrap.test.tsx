import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { api } from "../api";
import { createAppRouter } from "../router";
import { AppBootstrap } from "./AppBootstrap";
import { useApp } from "./AppContext";

vi.mock("../api", () => ({ api: { me: vi.fn(), settings: vi.fn() } }));
vi.mock("../router", () => ({ createAppRouter: vi.fn(() => ({})) }));
vi.mock("../live/OverviewStreamProvider", () => ({ OverviewStreamProvider: ({ children }: { children: React.ReactNode }) => children }));
vi.mock("react-router-dom", () => ({ RouterProvider: () => <AuthProbe /> }));
function AuthProbe() { const { auth } = useApp(); return <p>{auth.loggedIn ? auth.user : "guest"}</p>; }
afterEach(() => { cleanup(); vi.clearAllMocks(); });

it.each([false, true])("keeps the router stable when auth finishes loading (loggedIn=%s)", async (loggedIn) => {
  vi.mocked(api.me).mockResolvedValue({ loggedIn, authEnabled: true, user: "manager" });
  vi.mocked(api.settings).mockResolvedValue({ siteName: "Monitor", siteDescription: "" });
  render(<AppBootstrap />);
  expect(screen.getByText("正在读取监控状态…")).toBeInTheDocument();
  expect(await screen.findByText(loggedIn ? "manager" : "guest")).toBeInTheDocument();
  expect(createAppRouter).toHaveBeenCalledTimes(1);
  expect(createAppRouter).toHaveBeenCalledWith();
});
