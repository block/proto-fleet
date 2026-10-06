import { describe, expect, it } from "vitest";

import { formatDeviceType } from "@/protoFleet/features/infrastructure/deviceType";

describe("formatDeviceType", () => {
  it("labels a single fan", () => {
    expect(formatDeviceType({ deviceKind: "single_fan", fanCount: 1 })).toBe("Fan");
  });

  it("labels a fan group with its fan count", () => {
    expect(formatDeviceType({ deviceKind: "fan_group", fanCount: 12 })).toBe("Fan group (12 fans)");
    expect(formatDeviceType({ deviceKind: "fan_group", fanCount: 1 })).toBe("Fan group");
  });

  it("shows an unknown kind's raw wire value instead of mislabeling it", () => {
    expect(formatDeviceType({ deviceKind: "pump_station", fanCount: 0 })).toBe("pump_station");
  });
});
