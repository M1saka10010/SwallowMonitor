import { expect, it } from "vitest";
import { installationCommand } from "./clipboard";

it("keeps the legacy SwallowAgent install command contract", () => {
  const protocol = location.protocol === "https:" ? "wss" : "ws";
  expect(installationCommand("token-123")).toBe(`curl -fsSL https://raw.githubusercontent.com/M1saka10010/SwallowAgent/main/install.sh | bash -s -- ${protocol}://${location.host}/report token-123`);
});
