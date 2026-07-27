import { describe, expect, it } from "vitest";
import type { Usage } from "../../api/types";
import { buildTimeline, estimateSampleInterval } from "./chartData";

const usage = (timestamp: number) => ({ timestamp, cpuUsage: 1 } as Usage);

describe("chart timeline", () => {
  it("uses the lower quartile interval", () => expect(estimateSampleInterval([usage(0), usage(10), usage(20), usage(200)])).toBe(10));
  it("marks empty windows offline", () => expect(buildTimeline([], 0, 100).every((point) => point.offline)).toBe(true));
  it("inserts offline points around a large gap", () => {
    const timeline = buildTimeline([usage(100), usage(110), usage(300)], 100, 300);
    expect(timeline.filter((point) => point.offline)).toHaveLength(2);
  });
});
