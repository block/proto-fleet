import type { HTMLAttributes, ReactNode } from "react";
import { render, screen } from "@testing-library/react";
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

  it("blocks missing and no-op targets and hides unsupported automation", () => {
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
    expect(screen.queryByText("Auto-continue healthy batches")).not.toBeInTheDocument();
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
