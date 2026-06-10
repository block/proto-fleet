import { describe, expect, it } from "vitest";

import { importSettingsCurtailment, settingsRoutePrefetch } from "@/protoFleet/routePrefetch";

describe("settingsRoutePrefetch", () => {
  it("warms the URL-only curtailment settings page", () => {
    expect(settingsRoutePrefetch).toContain(importSettingsCurtailment);
  });
});
