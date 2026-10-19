import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { AppShell } from "./AppShell";

const auth = vi.hoisted(() => ({ loggedIn: true, authEnabled: true }));
vi.mock("./AppContext", () => ({ useApp: () => ({ auth, settings: { siteName: "Monitor" } }) }));
vi.mock("./ThemeProvider", () => ({ useTheme: () => ({ theme: "auto", setTheme: vi.fn() }) }));
vi.mock("../live/OverviewStreamProvider", () => ({ useOverviewStream: () => ({ connection: "open" }) }));
afterEach(cleanup);

it("submits logout using a same-origin POST form", async () => {
  auth.loggedIn = true;
  auth.authEnabled = true;
  render(<MemoryRouter><AppShell /></MemoryRouter>);
  const button = screen.getByRole("button", { name: "退出" });
  const form = button.closest("form")!;
  expect(form).toHaveAttribute("action", "/logout");
  expect(form).toHaveAttribute("method", "post");
  expect(screen.queryByRole("link", { name: "退出" })).not.toBeInTheDocument();
  const submit = vi.fn((event: Event) => event.preventDefault());
  form.addEventListener("submit", submit);
  await userEvent.setup().click(button);
  expect(submit).toHaveBeenCalledOnce();
});

it.each([[false, true], [true, false]])("hides logout for loggedIn=%s and authEnabled=%s", (loggedIn, authEnabled) => {
  Object.assign(auth, { loggedIn, authEnabled });
  render(<MemoryRouter><AppShell /></MemoryRouter>);
  expect(screen.queryByRole("button", { name: "退出" })).not.toBeInTheDocument();
  if (!loggedIn) expect(screen.getByRole("link", { name: "登录" })).toHaveAttribute("href", "/login");
});
