import { expect, it, vi } from "vitest";
import { createHashRouter } from "react-router-dom";
import { createAppRouter } from "./router";

vi.mock("react-router-dom", async (original) => ({ ...await original<typeof import("react-router-dom")>(), createHashRouter: vi.fn() }));

it("keeps overview eager and loads detail and all admin routes on demand", async () => {
  createAppRouter();
  const root = vi.mocked(createHashRouter).mock.calls[0][0][0];
  expect(root.errorElement).toBeTruthy();
  expect(root.hydrateFallbackElement).toBeTruthy();
  expect(root.children?.find((route) => route.index)?.lazy).toBeUndefined();
  const detail = root.children!.find((route) => route.path === "hosts/:publicId")!;
  const admin = root.children!.find((route) => route.path === "admin")!;
  for (const route of [detail, admin, ...admin.children!]) {
    expect(route.element).toBeUndefined();
    expect(typeof route.lazy).toBe("function");
    const loaded = await (route.lazy as () => Promise<{ Component: unknown }>)();
    expect(typeof loaded.Component).toBe("function");
  }
});
