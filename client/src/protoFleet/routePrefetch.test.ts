import { describe, expect, it } from "vitest";

import {
  globalRoutePrefetch,
  importMinerbotPage,
  importSettingsAgents,
  importSettingsCurtailment,
  settingsRoutePrefetch,
} from "@/protoFleet/routePrefetch";

describe("globalRoutePrefetch", () => {
  it("warms the Minerbot primary navigation page", () => {
    expect(globalRoutePrefetch).toContain(importMinerbotPage);
  });
});

describe("settingsRoutePrefetch", () => {
  it("warms the URL-only curtailment settings page", () => {
    expect(settingsRoutePrefetch).toContain(importSettingsCurtailment);
  });

  it("warms the Agents settings page", () => {
    expect(settingsRoutePrefetch).toContain(importSettingsAgents);
  });
});
