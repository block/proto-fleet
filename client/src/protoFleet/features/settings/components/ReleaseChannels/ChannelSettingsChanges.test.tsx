import { cleanup, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";

import { defaultBehavior } from "./behaviorUtils";
import ChannelSettingsChangesView from "./ChannelSettingsChanges";
import { getChannelSettingsChanges } from "./channelSettingsChangesUtils";
import {
  ReleaseChannelScopeSchema,
  RolloutAutomationThresholdsSchema,
  RolloutBehaviorSchema,
  RolloutMethod,
  RolloutOrder,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import type { ReleaseChannelDraft } from "@/protoFleet/api/useReleaseChannels";

afterEach(cleanup);

const ChannelSettingsChanges = ({
  before,
  after,
  minerNames,
}: {
  before: ReleaseChannelDraft;
  after: ReleaseChannelDraft;
  minerNames: Record<string, string>;
}) => <ChannelSettingsChangesView changes={getChannelSettingsChanges(before, after, minerNames)} />;

const draft = (patch: Partial<ReleaseChannelDraft> = {}): ReleaseChannelDraft => ({
  name: "Stable",
  description: "",
  scope: create(ReleaseChannelScopeSchema),
  behavior: defaultBehavior(),
  ...patch,
});
const row = (label: string) => within(screen.getByRole("rowheader", { name: label }).closest("tr")!);
const expectChange = (label: string, original: string, target: string) => {
  const cells = row(label).getAllByRole("cell");
  expect(cells.map((cell) => cell.textContent)).toEqual([original, target]);
};

describe("channel settings changes preview", () => {
  it("shows changed names and descriptions with their previous values", () => {
    render(
      <ChannelSettingsChanges
        before={draft()}
        after={draft({ name: "Canary", description: "Pilot miners" })}
        minerNames={{}}
      />,
    );
    expect(screen.getByRole("region", { name: "Channel settings changes" })).toBeInTheDocument();
    const table = within(screen.getByRole("table", { name: "Channel settings changes" }));
    expect(table.getAllByRole("columnheader").map((cell) => cell.textContent)).toEqual([
      "Setting",
      "Original",
      "Target",
    ]);
    expectChange("Name", "Stable", "Canary");
    expectChange("Description", "None", "Pilot miners");
    expect(screen.queryByText("Update method")).not.toBeInTheDocument();
  });

  it("hides whitespace-only, reordered selector and inactive draft changes", () => {
    const before = draft({ scope: create(ReleaseChannelScopeSchema, { siteIds: [1n, 2n] }) });
    const after = draft({
      name: " Stable \u0085",
      description: " ",
      scope: create(ReleaseChannelScopeSchema, { siteIds: [2n, 1n, 1n] }),
      behavior: create(RolloutBehaviorSchema, {
        ...defaultBehavior(),
        batchSize: 99,
        pilotSize: 50,
        waitBetweenBatchesSeconds: 5,
        reviewAfterEachBatch: true,
        autoContinueOnHealthyTelemetry: true,
        stabilizationSeconds: 5,
        controllerTimeoutSeconds: 20,
        thresholds: create(RolloutAutomationThresholdsSchema, { maxNewErrors: 10, minSampleCoveragePercent: 30 }),
      }),
    });
    const { container } = render(<ChannelSettingsChanges before={before} after={after} minerNames={{}} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("shows method, order, batch pacing and offline budget changes", () => {
    const after = draft({
      behavior: create(RolloutBehaviorSchema, {
        method: RolloutMethod.BATCHED,
        order: RolloutOrder.RANDOM,
        batchSize: 4,
        waitBetweenBatchesSeconds: 60,
        maxConcurrentOffline: 2,
      }),
    });
    render(<ChannelSettingsChanges before={draft()} after={after} minerNames={{}} />);
    expectChange("Update method", "Single batch", "Multiple batches");
    expectChange("Update order", "Least efficient first", "Random");
    expectChange("Batch size", "Not used", "4 miners");
    expectChange("Wait between batches", "Not used", "1m");
    expectChange("Offline limit", "Unlimited", "2 miners");
  });

  it("shows review gates, automation and every telemetry threshold including zero", () => {
    const before = draft({ behavior: create(RolloutBehaviorSchema, { method: RolloutMethod.BATCHED, batchSize: 4 }) });
    const after = draft({
      behavior: create(RolloutBehaviorSchema, {
        ...before.behavior,
        reviewAfterEachBatch: true,
        autoContinueOnHealthyTelemetry: true,
        stabilizationSeconds: 600,
        thresholds: create(RolloutAutomationThresholdsSchema, {
          maxHashrateDropPercent: 0,
          maxEfficiencyIncreasePercent: 20,
          maxTemperatureIncreaseCelsius: 5,
          maxNewErrors: 0,
          minSampleCoveragePercent: 75,
        }),
      }),
    });
    render(<ChannelSettingsChanges before={before} after={after} minerNames={{}} />);
    expectChange("Review gates", "None", "After each batch");
    expectChange("Continue automatically", "Not used", "When telemetry is healthy");
    expectChange("Stabilization time", "Not used", "10m");
    expectChange("Maximum hashrate drop", "Not used", "0%");
    expectChange("Maximum efficiency increase", "Not used", "20%");
    expectChange("Maximum temperature increase", "Not used", "5°C");
    expectChange("Maximum new errors", "Not used", "0");
    expectChange("Minimum sample coverage", "Not used", "75%");
  });

  it("shows removed limits and default sample coverage without conflating them with zero", () => {
    const before = draft({
      behavior: create(RolloutBehaviorSchema, {
        method: RolloutMethod.PILOT_THEN_CONTINUE,
        pilotSize: 1,
        autoContinueOnHealthyTelemetry: true,
        thresholds: create(RolloutAutomationThresholdsSchema, {
          maxHashrateDropPercent: 10,
          maxNewErrors: 0,
          minSampleCoveragePercent: 75,
        }),
      }),
    });
    const after = draft({
      behavior: create(RolloutBehaviorSchema, {
        ...before.behavior,
        pilotSize: 2,
        thresholds: create(RolloutAutomationThresholdsSchema, { maxHashrateDropPercent: 10 }),
      }),
    });
    render(<ChannelSettingsChanges before={before} after={after} minerNames={{}} />);
    expectChange("Pilot size", "1 miner", "2 miners");
    expectChange("Maximum new errors", "0", "No limit");
    expectChange("Minimum sample coverage", "75%", "100% (default)");
  });

  it("shows controller timeouts accurately when duration labels round to the same minute", () => {
    const before = draft({
      behavior: create(RolloutBehaviorSchema, { method: RolloutMethod.DELEGATED, controllerTimeoutSeconds: 61 }),
    });
    const after = draft({
      behavior: create(RolloutBehaviorSchema, { ...before.behavior, controllerTimeoutSeconds: 62 }),
    });
    render(<ChannelSettingsChanges before={before} after={after} minerNames={{}} />);
    expectChange("Controller timeout", "1m (61s)", "1m (62s)");
  });

  it("preserves small threshold differences in the confirmation", () => {
    const before = draft({
      behavior: create(RolloutBehaviorSchema, {
        method: RolloutMethod.PILOT_THEN_CONTINUE,
        pilotSize: 1,
        autoContinueOnHealthyTelemetry: true,
        thresholds: create(RolloutAutomationThresholdsSchema, { maxHashrateDropPercent: 0.0001 }),
      }),
    });
    const after = draft({
      behavior: create(RolloutBehaviorSchema, {
        ...before.behavior,
        thresholds: create(RolloutAutomationThresholdsSchema, { maxHashrateDropPercent: 0.0002 }),
      }),
    });
    render(<ChannelSettingsChanges before={before} after={after} minerNames={{}} />);
    expectChange("Maximum hashrate drop", "0.0001%", "0.0002%");
  });

  it("identifies same-count scope replacements for every selector and disambiguates miner names", () => {
    const before = draft({
      scope: create(ReleaseChannelScopeSchema, {
        siteIds: [1n],
        buildingIds: [2n],
        rackIds: [3n],
        groupIds: [4n],
        deviceIdentifiers: ["old"],
      }),
    });
    const after = draft({
      scope: create(ReleaseChannelScopeSchema, {
        siteIds: [11n],
        buildingIds: [12n],
        rackIds: [13n],
        groupIds: [14n],
        deviceIdentifiers: ["new", "missing"],
      }),
    });
    render(<ChannelSettingsChanges before={before} after={after} minerNames={{ old: "Rig", new: "Rig" }} />);
    expectChange("Applies to · Sites", "Site 1", "Site 11");
    expectChange("Applies to · Buildings", "Building 2", "Building 12");
    expectChange("Applies to · Racks", "Rack 3", "Rack 13");
    expectChange("Applies to · Groups", "Group 4", "Group 14");
    expectChange("Applies to · Miners", "Rig (old)", "Miner missing\nRig (new)");
  });

  it("shows complete original and target scope selections, including retained selectors and empty selections", () => {
    const before = draft({
      scope: create(ReleaseChannelScopeSchema, {
        siteIds: [1n, 2n],
        buildingIds: [4n],
      }),
    });
    const after = draft({
      scope: create(ReleaseChannelScopeSchema, {
        siteIds: [2n, 3n, 3n],
        groupIds: [5n],
      }),
    });
    render(<ChannelSettingsChanges before={before} after={after} minerNames={{}} />);
    expectChange("Applies to · Sites", "Site 1\nSite 2", "Site 2\nSite 3");
    expectChange("Applies to · Buildings", "Building 4", "None");
    expectChange("Applies to · Groups", "None", "Group 5");
    expect(screen.queryByRole("rowheader", { name: "Applies to · Racks" })).not.toBeInTheDocument();
    expect(screen.queryByRole("rowheader", { name: "Applies to · Miners" })).not.toBeInTheDocument();
  });
});
