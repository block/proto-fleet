import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { activeRigRollout } from "./ReleaseChannels.fixtures";
import RolloutDetailModal from "./RolloutDetailModal";
import RolloutMinersModal from "./RolloutMinersModal";
import {
  RolloutBehaviorSchema,
  RolloutDeviceCountsSchema,
  RolloutDevicePhase,
  RolloutDeviceSchema,
  RolloutEvidenceSchema,
  RolloutMethod,
  RolloutSchema,
  RolloutStage,
  RolloutState,
  RolloutStatus,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

const devices = [
  create(RolloutDeviceSchema, { deviceIdentifier: "batch-one", batch: 1, phase: RolloutDevicePhase.DONE }),
  create(RolloutDeviceSchema, { deviceIdentifier: "batch-two", batch: 2, phase: RolloutDevicePhase.DONE }),
  create(RolloutDeviceSchema, { deviceIdentifier: "remaining-done", phase: RolloutDevicePhase.DONE }),
  create(RolloutDeviceSchema, { deviceIdentifier: "late-joiner", phase: RolloutDevicePhase.QUEUED }),
];

const restRollout = create(RolloutSchema, {
  ...activeRigRollout,
  behavior: create(RolloutBehaviorSchema, { method: RolloutMethod.BATCHED, batchSize: 1 }),
  batchCount: 2,
  currentBatch: 1,
  deviceCount: devices.length,
  deviceCounts: create(RolloutDeviceCountsSchema, { done: 3, queued: 1 }),
  currentBatchCounts: create(RolloutDeviceCountsSchema),
  evidence: create(RolloutEvidenceSchema, { devicesTotal: 2, verified: 1, online: 1, hashing: 1, baselineHashing: 2 }),
});

describe("rollout evidence scope", () => {
  it("distinguishes overall REST progress from remaining-miner evidence in the detail and miner views", async () => {
    const listRolloutDevices = vi.fn().mockResolvedValue(devices);
    render(
      <RolloutDetailModal
        rollout={restRollout}
        minerNames={{}}
        listRolloutDevices={listRolloutDevices}
        onClose={vi.fn()}
        onContinue={vi.fn().mockResolvedValue(undefined)}
        onPause={vi.fn().mockResolvedValue(undefined)}
        onResume={vi.fn().mockResolvedValue(undefined)}
        onCancel={vi.fn()}
        onRollback={vi.fn()}
        onRetryFailed={vi.fn().mockResolvedValue(undefined)}
      />,
    );

    expect(screen.getByTestId("rollout-detail-progress")).toHaveTextContent(
      "Overall progress: 3 of 4 miners updated (75%)",
    );
    expect(screen.getByTestId("evidence-online")).toHaveTextContent("1 of 2");
    expect(screen.getByTestId("evidence-online")).toHaveTextContent("Evidence: remaining miners");
    expect(screen.getByTestId("rollout-evidence")).toHaveTextContent("Telemetry evidence: remaining miners (2 miners)");

    fireEvent.click(screen.getByTestId("view-rollout-more-actions-trigger"));
    fireEvent.click(screen.getByTestId("view-rollout-view-miners-action"));
    expect(
      await screen.findByText("4 miners in this update; evidence scope: remaining miners (2 miners)"),
    ).toBeInTheDocument();
    expect(await screen.findByText("batch-one")).toBeInTheDocument();
    expect(screen.getByText("batch-two")).toBeInTheDocument();
    expect(screen.getByText("late-joiner")).toBeInTheDocument();
    expect(listRolloutDevices).toHaveBeenCalledExactlyOnceWith(restRollout.id);
  });

  it("identifies the current batch while a staged rollout is waiting between batches", async () => {
    const rollout = create(RolloutSchema, {
      ...restRollout,
      stage: RolloutStage.WAITING,
      currentBatch: 0,
      currentBatchCounts: create(RolloutDeviceCountsSchema, { done: 1 }),
      evidence: create(RolloutEvidenceSchema, { devicesTotal: 1, verified: 1 }),
    });
    render(
      <RolloutMinersModal
        rollout={rollout}
        minerNames={{}}
        listRolloutDevices={vi.fn().mockResolvedValue(devices)}
        onClose={vi.fn()}
      />,
    );
    expect(
      await screen.findByText("4 miners in this update; evidence scope: batch 1 of 2 (1 miner)"),
    ).toBeInTheDocument();
  });

  it.each([RolloutMethod.ALL_AT_ONCE, RolloutMethod.DELEGATED])(
    "labels the whole update as evidence scope for unbatched method %s",
    async (method) => {
      const rollout = create(RolloutSchema, {
        ...activeRigRollout,
        behavior: create(RolloutBehaviorSchema, { method }),
      });
      const unbatchedDevices = devices.map((device) => create(RolloutDeviceSchema, { ...device, batch: 0 }));
      render(
        <RolloutMinersModal
          rollout={rollout}
          minerNames={{}}
          listRolloutDevices={vi.fn().mockResolvedValue(unbatchedDevices)}
          onClose={vi.fn()}
        />,
      );
      expect(
        await screen.findByText("4 miners in this update; evidence scope: all miners (4 miners)"),
      ).toBeInTheDocument();
      expect(screen.queryByText(/current batch/)).not.toBeInTheDocument();
    },
  );

  it("does not claim a current evidence scope for a completed staged rollout", async () => {
    const rollout = create(RolloutSchema, {
      ...restRollout,
      status: RolloutStatus.COMPLETED,
      state: RolloutState.COMPLETED,
      deviceCounts: create(RolloutDeviceCountsSchema, { done: 4 }),
      evidence: undefined,
    });
    const completedDevices = devices.map((device) =>
      create(RolloutDeviceSchema, { ...device, phase: RolloutDevicePhase.DONE }),
    );
    render(
      <RolloutMinersModal
        rollout={rollout}
        minerNames={{}}
        listRolloutDevices={vi.fn().mockResolvedValue(completedDevices)}
        onClose={vi.fn()}
      />,
    );
    expect(await screen.findByText("4 miners in this update")).toBeInTheDocument();
    expect(screen.queryByText(/evidence scope/)).not.toBeInTheDocument();
    expect(screen.queryByText(/current batch/)).not.toBeInTheDocument();
    expect(await within(screen.getByTestId("rollout-miners-modal")).findByText("batch-one")).toBeInTheDocument();
  });
});
