import { describe, expect, it } from "vitest";

import { importSettingsAgents, importSettingsCurtailment, settingsRoutePrefetch } from "@/protoFleet/routePrefetch";

describe("settingsRoutePrefetch", () => {
  it("warms the URL-only curtailment settings page", () => {
    expect(settingsRoutePrefetch).toContain(importSettingsCurtailment);
  });

  it("warms the Agents settings page", () => {
    expect(settingsRoutePrefetch).toContain(importSettingsAgents);
  });
});
