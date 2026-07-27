import { expect, it } from "vitest";
import { parseLiveEvent } from "./events";

it("parses status and rejects malformed events", () => {
  expect(parseLiveEvent('{"type":"status","publicId":"a","online":true}')).toEqual({ type: "status", publicId: "a", online: true });
  expect(parseLiveEvent("not-json")).toBeNull();
  expect(parseLiveEvent('{"type":"unknown","publicId":"a"}')).toBeNull();
});
