import { afterEach, expect, it, vi } from "vitest";
import { copyText } from "./clipboard";

afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks(); document.body.innerHTML = ""; });

it("copies through the secure clipboard API", async () => {
  const writeText = vi.fn().mockResolvedValue(undefined);
  vi.stubGlobal("navigator", { clipboard: { writeText } });
  vi.stubGlobal("isSecureContext", true);
  await copyText("public-key");
  expect(writeText).toHaveBeenCalledWith("public-key");
  expect(document.querySelector("textarea")).toBeNull();
});

it.each([true, false])("cleans up the fallback when copy returns %s", async (success) => {
  vi.stubGlobal("isSecureContext", false);
  Object.defineProperty(document, "execCommand", { configurable: true, value: vi.fn(() => success) });
  if (success) await copyText("public-key");
  else await expect(copyText("public-key")).rejects.toThrow("复制失败");
  expect(document.execCommand).toHaveBeenCalledWith("copy");
  expect(document.querySelector("textarea")).toBeNull();
});

it("reports clipboard API errors", async () => {
  vi.stubGlobal("navigator", { clipboard: { writeText: vi.fn().mockRejectedValue(new Error("denied")) } });
  vi.stubGlobal("isSecureContext", true);
  await expect(copyText("key")).rejects.toThrow("denied");
});
