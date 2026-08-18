import { describe, expect, it } from "vitest";

import { getAsicId } from "./getAsicId";

describe("getAsicId", () => {
  it("uses the normalized hashboard namespace shared by hardware and telemetry", () => {
    expect(getAsicId("HB-1", 42)).toBe("serial:HB-1:asic:42");
    expect(getAsicId("serial:HB-1", 42)).toBe("serial:HB-1:asic:42");
  });
});
