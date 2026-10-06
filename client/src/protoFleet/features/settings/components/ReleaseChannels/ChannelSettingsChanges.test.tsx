import type { ComponentProps } from "react";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
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
  onViewMiners = vi.fn(),
}: {
  before: ReleaseChannelDraft;
  after: ReleaseChannelDraft;
  minerNames: Record<string, string>;
  onViewMiners?: ComponentProps<typeof ChannelSettingsChangesView>["onViewMiners"];
}) => (
  <ChannelSettingsChangesView
    changes={getChannelSettingsChanges(before, after, minerNames)}
    onViewMiners={onViewMiners}
  />
);

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

  it("shows scope changes and keeps full miner identities behind a single action", () => {
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
    const onViewMiners = vi.fn();
    render(
      <ChannelSettingsChanges
        before={before}
        after={after}
        minerNames={{ old: "Rig", new: "Rig" }}
        onViewMiners={onViewMiners}
      />,
    );
    expectChange("Applies to · Sites", "Site 1", "Site 11");
    expectChange("Applies to · Buildings", "Building 2", "Building 12");
    expectChange("Applies to · Racks", "Rack 3", "Rack 13");
    expectChange("Applies to · Groups", "Group 4", "Group 14");
    expectChange("Applies to · Miners", "1 miner", "2 miners");
    expect(screen.getByText("2 added · 1 removed")).toBeInTheDocument();
    expect(screen.queryByText("Rig")).not.toBeInTheDocument();
    const button = screen.getByRole("button", { name: "View miners" });
    fireEvent.click(button);
    expect(onViewMiners).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({
        originalCount: 1,
        targetCount: 2,
        addedCount: 2,
        removedCount: 1,
        miners: expect.arrayContaining([
          expect.objectContaining({ identifier: "old", name: "Rig", original: true, target: false }),
          expect.objectContaining({ identifier: "new", name: "Rig", original: false, target: true }),
          expect.objectContaining({ identifier: "missing", original: false, target: true }),
        ]),
      }),
      button,
    );
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

  it("makes same-size miner replacements visible without expanding the original and target lists", () => {
    const before = draft({ scope: create(ReleaseChannelScopeSchema, { deviceIdentifiers: ["retained", "removed"] }) });
    const after = draft({ scope: create(ReleaseChannelScopeSchema, { deviceIdentifiers: ["retained", "added"] }) });
    render(<ChannelSettingsChanges before={before} after={after} minerNames={{}} />);
    expectChange("Applies to · Miners", "2 miners", "2 miners");
    expect(screen.getByText("1 added · 1 removed")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "View miners" })).toBeInTheDocument();
  });

  it("counts unique selected miners and preserves retained miners in the detail snapshot", () => {
    const before = draft({
      scope: create(ReleaseChannelScopeSchema, { deviceIdentifiers: ["retained", "old", "old"] }),
    });
    const after = draft({
      scope: create(ReleaseChannelScopeSchema, { deviceIdentifiers: ["new", "retained", "new"] }),
    });
    const changes = getChannelSettingsChanges(before, after, { old: "Original", retained: "Retained", new: "Target" });
    expect(changes.minerChanges).toMatchObject({ originalCount: 2, targetCount: 2, addedCount: 1, removedCount: 1 });
    expect(changes.minerChanges?.miners).toHaveLength(3);
    expect(changes.minerChanges?.miners).toEqual(
      expect.arrayContaining([
        { identifier: "retained", name: "Retained", original: true, target: true },
        { identifier: "old", name: "Original", original: true, target: false },
        { identifier: "new", name: "Target", original: false, target: true },
      ]),
    );
  });

  it("keeps a large miner selection compact and passes the complete snapshot to the details action", () => {
    const identifiers = Array.from({ length: 125 }, (_, index) => `miner-${index}`);
    const names = Object.fromEntries(identifiers.map((identifier, index) => [identifier, `Named miner ${index}`]));
    const before = draft({ scope: create(ReleaseChannelScopeSchema, { deviceIdentifiers: identifiers }) });
    const after = draft({ scope: create(ReleaseChannelScopeSchema, { deviceIdentifiers: identifiers.slice(1) }) });
    const changes = getChannelSettingsChanges(before, after, names);
    // A refresh must not rename the already-captured preview while it is being confirmed.
    names["miner-0"] = "Renamed after preview";
    const onViewMiners = vi.fn();
    render(<ChannelSettingsChangesView changes={changes} onViewMiners={onViewMiners} />);
    expectChange("Applies to · Miners", "125 miners", "124 miners");
    expect(screen.queryByText(/Named miner/)).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "View miners" }));
    const details = onViewMiners.mock.calls[0][0];
    expect(details.miners).toHaveLength(125);
    expect(details.miners).toContainEqual({
      identifier: "miner-0",
      name: "Named miner 0",
      original: true,
      target: false,
    });
  });

  it.each([
    { original: [], target: ["new"], before: "0 miners", after: "1 miner" },
    { original: ["old"], target: [], before: "1 miner", after: "0 miners" },
  ])("shows an explicit zero count for an empty side ($before → $after)", ({ original, target, before, after }) => {
    render(
      <ChannelSettingsChanges
        before={draft({ scope: create(ReleaseChannelScopeSchema, { deviceIdentifiers: original }) })}
        after={draft({ scope: create(ReleaseChannelScopeSchema, { deviceIdentifiers: target }) })}
        minerNames={{}}
      />,
    );
    expectChange("Applies to · Miners", before, after);
  });

  it("does not show miner details when only ordering or duplicate selections change", () => {
    const before = draft({ scope: create(ReleaseChannelScopeSchema, { deviceIdentifiers: ["one", "two"] }) });
    const after = draft({ scope: create(ReleaseChannelScopeSchema, { deviceIdentifiers: ["two", "one", "one"] }) });
    expect(getChannelSettingsChanges(before, after, {}).minerChanges).toBeNull();
    const { container } = render(<ChannelSettingsChanges before={before} after={after} minerNames={{}} />);
    expect(container).toBeEmptyDOMElement();
  });

  it.each(["constructor", "toString", "__proto__"])("uses only stored miner names for identifier %s", (identifier) => {
    const before = draft();
    const after = draft({ scope: create(ReleaseChannelScopeSchema, { deviceIdentifiers: [identifier] }) });
    expect(getChannelSettingsChanges(before, after, {}).minerChanges?.miners).toEqual([
      { identifier, name: identifier, original: false, target: true },
    ]);
    expect(
      getChannelSettingsChanges(before, after, Object.fromEntries([[identifier, "Named miner"]])).minerChanges?.miners,
    ).toEqual([{ identifier, name: "Named miner", original: false, target: true }]);
  });
});
