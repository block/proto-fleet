import type { HTMLAttributes, ReactNode } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import StartRolloutLaneModal from "./StartRolloutLaneModal";
import type { FirmwareFileInfo } from "@/protoFleet/api/useFirmwareApi";
import type { RolloutLane } from "@/protoFleet/features/rollout/rolloutTypes";

vi.mock("motion/react", () => ({
  AnimatePresence: ({ children }: { children: ReactNode }) => children,
  motion: {
    div: ({ children, ...props }: HTMLAttributes<HTMLDivElement>) => <div {...props}>{children}</div>,
  },
}));

const lane: RolloutLane = {
  id: "15bc6181-07d8-45ac-8424-50b5e938b871",
  label: "Stable production",
  description: "",
  currentChannelId: 41n,
  revision: 2n,
  channels: [],
  memberCount: 2,
  memberIdentifiers: ["miner-1", "miner-2"],
  firmwareConvergence: {
    totalCount: 2,
    pendingCount: 0,
    updatingCount: 0,
    verifyingCount: 0,
    confirmedCount: 2,
    attentionCount: 0,
    members: [],
  },
  currentReleaseTargets: [
    {
      firmwareFileId: "alpha-1",
      targetManufacturer: "Proto",
      targetModel: "Alpha",
      firmwareVersion: "1.0.0",
      sha256: "a",
    },
    {
      firmwareFileId: "beta-1",
      targetManufacturer: "Proto",
      targetModel: "Beta",
      firmwareVersion: "1.0.0",
      sha256: "b",
    },
  ],
  models: [],
  scalarProjectionAvailable: true,
  topologyEnabled: false,
};

const files: FirmwareFileInfo[] = [
  {
    id: "alpha-1",
    filename: "alpha-1.swu",
    size: 100,
    uploaded_at: "2026-08-18T00:00:00Z",
    target_manufacturer: "Proto",
    target_model: "Alpha",
    firmware_version: "1.0.0",
  },
  {
    id: "alpha-2",
    filename: "alpha-2.swu",
    size: 100,
    uploaded_at: "2026-08-18T00:00:00Z",
    target_manufacturer: "Proto",
    target_model: "Alpha",
    firmware_version: "2.0.0",
  },
  {
    id: "beta-2",
    filename: "beta-2.swu",
    size: 100,
    uploaded_at: "2026-08-18T00:00:00Z",
    target_manufacturer: "Proto",
    target_model: "Beta",
    firmware_version: "2.0.0",
  },
];

describe("StartRolloutLaneModal", () => {
  it("submits every selected non-empty model as an independent plan", async () => {
    const user = userEvent.setup();
    const onStart = vi.fn();
    const modelLane: RolloutLane = {
      ...lane,
      topologyEnabled: true,
      firmwareConvergence: {
        ...lane.firmwareConvergence,
        confirmedCount: 1,
        updatingCount: 1,
      },
      models: [
        {
          id: "92c1db7c-7b32-4bab-8394-d773565ce5ae",
          modelIdentityKey: "v1:5:proto:5:alpha",
          revision: 3n,
          manufacturer: "Proto",
          model: "Alpha",
          currentChannelId: 41n,
          currentFirmwareTarget: {
            releaseTargetId: 1n,
            releaseSetId: 1n,
            firmwareFileId: "alpha-1",
            firmwareVersion: "1.0.0",
            sha256: "a",
          },
          memberCount: 1,
          memberIdentifiers: ["miner-1"],
          bindings: { activeCount: 1, historicalCount: 0 },
          firmwareConvergence: { ...lane.firmwareConvergence, totalCount: 1, confirmedCount: 1 },
          channels: [],
          compatibility: "compatible",
        },
        {
          id: "6d057fb4-590d-49f7-aa19-ee1d7c832ae8",
          modelIdentityKey: "v1:5:proto:4:beta",
          revision: 5n,
          manufacturer: "Proto",
          model: "Beta",
          currentChannelId: 41n,
          currentFirmwareTarget: {
            releaseTargetId: 2n,
            releaseSetId: 1n,
            firmwareFileId: "beta-1",
            firmwareVersion: "1.0.0",
            sha256: "b",
          },
          memberCount: 1,
          memberIdentifiers: ["miner-2"],
          bindings: { activeCount: 1, historicalCount: 0 },
          firmwareConvergence: { ...lane.firmwareConvergence, totalCount: 1, confirmedCount: 1 },
          channels: [],
          compatibility: "compatible",
        },
      ],
    };
    render(
      <StartRolloutLaneModal
        open
        lane={modelLane}
        files={files}
        isSubmitting={false}
        onDismiss={vi.fn()}
        onStart={onStart}
      />,
    );

    await user.click(screen.getByRole("checkbox", { name: /Proto Beta/ }));
    await user.type(screen.getByLabelText("Name"), "Alpha and Beta 2.0.0");
    await user.type(screen.getByLabelText("Reason"), "Update both models");
    await user.click(screen.getByRole("button", { name: "Start rollout" }));

    expect(onStart).toHaveBeenCalledWith({
      laneId: modelLane.id,
      name: "Alpha and Beta 2.0.0",
      reason: "Update both models",
      modelPlans: [
        {
          laneModelId: "92c1db7c-7b32-4bab-8394-d773565ce5ae",
          expectedModelRevision: 3n,
          firmwareFileId: "alpha-2",
          batches: [{ label: "Batch 1", members: [{ deviceIdentifier: "miner-1" }] }],
        },
        {
          laneModelId: "6d057fb4-590d-49f7-aa19-ee1d7c832ae8",
          expectedModelRevision: 5n,
          firmwareFileId: "beta-2",
          batches: [{ label: "Batch 1", members: [{ deviceIdentifier: "miner-2" }] }],
        },
      ],
    });
  });

  it("blocks an empty lane with the settled guidance", () => {
    render(
      <StartRolloutLaneModal
        open
        lane={{
          ...lane,
          memberCount: 0,
          memberIdentifiers: [],
          firmwareConvergence: {
            ...lane.firmwareConvergence,
            totalCount: 0,
            confirmedCount: 0,
          },
        }}
        files={files}
        isSubmitting={false}
        onDismiss={vi.fn()}
        onStart={vi.fn()}
      />,
    );

    expect(screen.getByText("Add miners before starting a rollout.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Start rollout" })).toBeDisabled();
  });

  it("blocks missing and no-op targets without exposing unsupported policy fields", () => {
    render(
      <StartRolloutLaneModal
        open
        lane={lane}
        files={[files[0]]}
        isSubmitting={false}
        onDismiss={vi.fn()}
        onStart={vi.fn()}
      />,
    );

    expect(screen.getAllByText(/matches the current release/).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/target file is required/).length).toBeGreaterThan(0);
    expect(screen.getByRole("button", { name: "Start rollout" })).toBeDisabled();
    expect(screen.getByRole("checkbox", { name: "Auto-continue healthy batches" })).toBeInTheDocument();
    expect(screen.queryByLabelText(/efficiency/i)).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/temperature/i)).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/errors/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/schedule/i)).not.toBeInTheDocument();
    expect(screen.getByText(/manual review gate/)).toBeInTheDocument();
  });

  it("submits selected files with the frozen pilot assignment", async () => {
    const user = userEvent.setup();
    const onStart = vi.fn();
    render(
      <StartRolloutLaneModal
        open
        lane={lane}
        files={files}
        isSubmitting={false}
        onDismiss={vi.fn()}
        onStart={onStart}
      />,
    );

    await user.type(screen.getByLabelText("Name"), "Production 2.0.0");
    await user.type(screen.getByLabelText("Reason"), "Validated release");
    await user.click(screen.getByRole("button", { name: "Start rollout" }));

    expect(onStart).toHaveBeenCalledWith({
      laneId: lane.id,
      name: "Production 2.0.0",
      firmwareFileIds: ["alpha-2", "beta-2"],
      reason: "Validated release",
      batches: [
        { label: "Pilot", members: [{ deviceIdentifier: "miner-1" }] },
        { label: "Remaining", members: [{ deviceIdentifier: "miner-2" }] },
      ],
    });
  });

  it("offers rollout-level automation for pilot and batched multi-batch plans", async () => {
    const user = userEvent.setup();
    render(
      <StartRolloutLaneModal
        open
        lane={lane}
        files={files}
        isSubmitting={false}
        onDismiss={vi.fn()}
        onStart={vi.fn()}
      />,
    );

    expect(screen.getByRole("checkbox", { name: "Auto-continue healthy batches" })).toBeInTheDocument();
    const method = screen.getByRole("button", { name: "Method" });
    const triggerRect = vi.spyOn(method.parentElement!, "getBoundingClientRect").mockReturnValue({
      x: 0,
      y: 100,
      width: 320,
      height: 56,
      top: 100,
      left: 0,
      bottom: 156,
      right: 320,
      toJSON: () => ({}),
    } as DOMRect);
    await user.click(method);
    await user.click(screen.getByRole("option", { name: "Multiple batches" }));
    triggerRect.mockRestore();
    expect(screen.getByRole("checkbox", { name: "Auto-continue healthy batches" })).toBeInTheDocument();
  });

  it("prefills policy defaults and submits exact integer basis points and duration", async () => {
    const user = userEvent.setup();
    const onStart = vi.fn();
    render(
      <StartRolloutLaneModal
        open
        lane={lane}
        files={files}
        isSubmitting={false}
        onDismiss={vi.fn()}
        onStart={onStart}
      />,
    );

    await user.type(screen.getByLabelText("Name"), "Production 2.0.0");
    await user.type(screen.getByLabelText("Reason"), "Validated release");
    await user.click(screen.getByRole("checkbox", { name: "Auto-continue healthy batches" }));
    expect(screen.getByLabelText("Maximum hashrate drop")).toHaveValue(0.1);
    expect(screen.getByLabelText("Healthy duration")).toHaveValue(30);

    await user.clear(screen.getByLabelText("Maximum hashrate drop"));
    await user.type(screen.getByLabelText("Maximum hashrate drop"), "12.3");
    await user.clear(screen.getByLabelText("Healthy duration"));
    await user.type(screen.getByLabelText("Healthy duration"), "40");
    await user.click(screen.getByRole("button", { name: "Start rollout" }));

    expect(onStart).toHaveBeenCalledWith(
      expect.objectContaining({
        hashratePolicy: {
          maxDropBasisPoints: 1_230,
          healthyDurationSeconds: 40,
        },
      }),
    );
  });

  it("disabling automation after enabling it omits the policy", async () => {
    const user = userEvent.setup();
    const onStart = vi.fn();
    render(
      <StartRolloutLaneModal
        open
        lane={lane}
        files={files}
        isSubmitting={false}
        onDismiss={vi.fn()}
        onStart={onStart}
      />,
    );

    await user.type(screen.getByLabelText("Name"), "Production 2.0.0");
    await user.type(screen.getByLabelText("Reason"), "Validated release");
    const automation = screen.getByRole("checkbox", { name: "Auto-continue healthy batches" });
    await user.click(automation);
    await user.click(automation);
    await user.click(screen.getByRole("button", { name: "Start rollout" }));

    expect(onStart).toHaveBeenCalledOnce();
    expect(onStart.mock.calls[0][0]).not.toHaveProperty("hashratePolicy");
  });

  it.each([
    ["Maximum hashrate drop", "0", true],
    ["Maximum hashrate drop", "100", true],
    ["Maximum hashrate drop", "-0.1", false],
    ["Maximum hashrate drop", "100.1", false],
    ["Maximum hashrate drop", "0.15", false],
    ["Healthy duration", "10", true],
    ["Healthy duration", "1800", true],
    ["Healthy duration", "0", false],
    ["Healthy duration", "1810", false],
    ["Healthy duration", "15", false],
    ["Healthy duration", "10.5", false],
  ] as const)("validates %s value %s at the exact policy boundaries", async (label, value, valid) => {
    const user = userEvent.setup();
    render(
      <StartRolloutLaneModal
        open
        lane={lane}
        files={files}
        isSubmitting={false}
        onDismiss={vi.fn()}
        onStart={vi.fn()}
      />,
    );

    await user.type(screen.getByLabelText("Name"), "Production 2.0.0");
    await user.type(screen.getByLabelText("Reason"), "Validated release");
    await user.click(screen.getByRole("checkbox", { name: "Auto-continue healthy batches" }));
    fireEvent.change(screen.getByLabelText(label), { target: { value } });

    if (valid) {
      expect(screen.getByLabelText(label)).not.toHaveAttribute("aria-invalid");
      expect(screen.getByRole("button", { name: "Start rollout" })).toBeEnabled();
    } else {
      expect(screen.getByLabelText(label)).toHaveAttribute("aria-invalid", "true");
      expect(screen.getByRole("button", { name: "Start rollout" })).toBeDisabled();
    }
  });

  it("associates inline policy errors and preserves logical keyboard order in a one-column layout", async () => {
    const user = userEvent.setup();
    render(
      <StartRolloutLaneModal
        open
        lane={lane}
        files={files}
        isSubmitting={false}
        onDismiss={vi.fn()}
        onStart={vi.fn()}
      />,
    );

    const automation = screen.getByRole("checkbox", { name: "Auto-continue healthy batches" });
    await user.click(automation);
    const maxDrop = screen.getByLabelText("Maximum hashrate drop");
    const duration = screen.getByLabelText("Healthy duration");
    fireEvent.change(maxDrop, { target: { value: "0.15" } });
    fireEvent.change(duration, { target: { value: "15" } });

    expect(maxDrop).toHaveAccessibleDescription("Enter 0 to 100% in 0.1% increments.");
    expect(duration).toHaveAccessibleDescription("Enter 10 to 1,800 seconds in 10-second increments.");
    expect(screen.getByTestId("hashrate-policy-fields")).not.toHaveClass("tablet:grid-cols-2");
    automation.focus();
    await user.tab();
    expect(maxDrop).toHaveFocus();
    await user.tab();
    expect(duration).toHaveFocus();
  });

  it("renders request errors and a locked loading state", () => {
    render(
      <StartRolloutLaneModal
        open
        lane={lane}
        files={files}
        isSubmitting
        error="Source membership changed"
        onDismiss={vi.fn()}
        onStart={vi.fn()}
      />,
    );

    expect(screen.getByText("Rollout could not be started")).toBeInTheDocument();
    expect(screen.getByText("Source membership changed")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Starting..." })).toBeDisabled();
  });
});
