import { afterEach, expect, it, vi } from "vitest";
import { requestJson } from "./client";

afterEach(() => vi.unstubAllGlobals());

it.each([undefined, "force-cache"] as const)("disables caching even when callers request %s", async (cache) => {
  const fetchMock = vi.fn().mockResolvedValue(new Response('{"ok":true}'));
  vi.stubGlobal("fetch", fetchMock);
  await expect(requestJson("/api/settings", { cache })).resolves.toEqual({ ok: true });
  expect(fetchMock).toHaveBeenCalledWith("/api/settings", expect.objectContaining({ cache: "no-store" }));
});
