import { describe, expect, test } from "vitest";
import { getFanId, getHashboardId, getPsuId } from "./entityIds";

describe("canonical entity IDs", () => {
  test("builds stable root-module component IDs", () => {
    expect(getPsuId(2)).toBe("module:root:psu:2");
    expect(getFanId(3)).toBe("module:root:fan:3");
  });

  test("does not double-prefix hashboard serial IDs", () => {
    expect(getHashboardId("HB-1")).toBe("serial:HB-1");
    expect(getHashboardId("serial:HB-1")).toBe("serial:HB-1");
    expect(getHashboardId("26:B0:3F:F0:D7:92")).toBe("mac:26:B0:3F:F0:D7:92");
    expect(getHashboardId("mac:26:B0:3F:F0:D7:92")).toBe("mac:26:B0:3F:F0:D7:92");
  });
});
