import { describe, expect, it } from "vitest";

import { importSettingsCurtailment, settingsRoutePrefetch } from "@/protoFleet/routePrefetch";

describe("settingsRoutePrefetch", () => {
  it("warms the curtailment settings tab", () => {
    expect(settingsRoutePrefetch).toContain(importSettingsCurtailment);
  });
});
