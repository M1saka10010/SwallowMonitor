import { describe, expect, it } from "vitest";
import { formatBytes, percentage } from "./format";

describe("formatBytes", () => {
  it("formats boundaries", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(1024)).toBe("1.0 KB");
    expect(formatBytes(1024 ** 3 * 1.5)).toBe("1.5 GB");
  });
  it("handles invalid values", () => expect(formatBytes(Number.NaN)).toBe("0 B"));
});

it("avoids division by zero", () => expect(percentage(10, 0)).toBe(0));
