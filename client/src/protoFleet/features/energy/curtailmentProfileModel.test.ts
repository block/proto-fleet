// @vitest-environment node
import { describe, expect, it } from "vitest";
import { defaultValues } from "./curtailmentFormValidation";
import {
  createCurtailmentFormValuesFromResponseProfile,
  createCurtailmentResponseProfileOption,
  createResponseProfileFormValuesFromCurtailmentValues,
} from "./curtailmentProfileModel";

describe("shared profile form model", () => {
  it("uses the same topology scope for Settings and a saved Energy plan", () => {
    const form = createResponseProfileFormValuesFromCurtailmentValues({
      ...defaultValues,
      scopeType: "rack",
      rackTargetIds: ["21", "22"],
      siteSelection: "site",
      siteId: "2",
      siteIds: ["2"],
      siteNamesById: { "2": "North" },
      reason: "Rack shutdown",
    });
    const settings = createCurtailmentFormValuesFromResponseProfile(form);
    const energy = createCurtailmentResponseProfileOption({
      id: "7",
      name: form.name,
      revision: "current",
      targetSummary: "100% reduction",
      scope: "2 racks",
      selectionStrategy: "Least efficient first",
      restoreBehavior: "Restore in batches",
      isAutomationReady: true,
      formValues: form,
    });
    expect(settings).toMatchObject({
      scopeType: "rack",
      rackTargetIds: ["21", "22"],
      siteIds: ["2"],
      siteNamesById: { "2": "North" },
    });
    expect(energy.values).toMatchObject({
      scopeType: settings.scopeType,
      scopeId: settings.scopeId,
      rackTargetIds: settings.rackTargetIds,
      siteIds: settings.siteIds,
    });
  });

  it("normalizes all-miner profiles without retaining stale site or device selections", () => {
    const form = createResponseProfileFormValuesFromCurtailmentValues({
      ...defaultValues,
      minerSelectionMode: "all",
      siteSelection: "site",
      siteId: "2",
      siteIds: ["2"],
      deviceIdentifiers: ["old-miner"],
    });
    expect(form).toMatchObject({
      minerSelectionMode: "all",
      siteSelection: "allSites",
      siteId: "",
      siteIds: [],
      siteNamesById: {},
      deviceIdentifiers: [],
    });
    expect(createCurtailmentFormValuesFromResponseProfile(form)).toMatchObject({
      scopeType: "wholeOrg",
      scopeId: "whole-org",
      siteIds: [],
      deviceIdentifiers: [],
    });
  });
});
