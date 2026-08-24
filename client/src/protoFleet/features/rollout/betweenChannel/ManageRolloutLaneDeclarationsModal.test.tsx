import type { HTMLAttributes, ReactNode } from "react";
import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import ManageRolloutLaneDeclarationsModal from "./ManageRolloutLaneDeclarationsModal";
import type { FirmwareFileInfo } from "@/protoFleet/api/useFirmwareApi";
import type { RolloutLane } from "@/protoFleet/features/rollout/rolloutTypes";

vi.mock("motion/react", () => ({
  AnimatePresence: ({ children }: { children: ReactNode }) => children,
  motion: {
    div: ({ children, ...props }: HTMLAttributes<HTMLDivElement>) => <div {...props}>{children}</div>,
  },
}));

vi.mock("@/protoFleet/features/settings/components/Schedules/MinerSelectionModal", () => ({
  default: () => null,
}));

const lane: RolloutLane = {
  id: "lane-1",
  label: "Production",
  description: "",
  currentChannelId: 10n,
  revision: 8n,
  channels: [],
  memberCount: 1,
  memberIdentifiers: [],
  currentReleaseTargets: [],
  firmwareConvergence: {
    totalCount: 1,
    pendingCount: 0,
    updatingCount: 0,
    verifyingCount: 0,
    confirmedCount: 1,
    attentionCount: 0,
    members: [],
  },
  topologyEnabled: true,
  scalarProjectionAvailable: false,
  models: [
    {
      id: "proto-model",
      modelIdentityKey: "v1:5:proto:5:alpha",
      revision: 2n,
      manufacturer: "Proto",
      model: "Alpha",
      currentChannelId: 10n,
      memberCount: 1,
      bindings: { activeCount: 1, historicalCount: 0 },
      firmwareConvergence: {
        totalCount: 1,
        pendingCount: 0,
        updatingCount: 0,
        verifyingCount: 0,
        confirmedCount: 1,
        attentionCount: 0,
        members: [],
      },
      channels: [],
      compatibility: "compatible",
    },
    {
      id: "empty-antminer",
      modelIdentityKey: "v1:8:antminer:3:s21",
      revision: 4n,
      manufacturer: "Antminer",
      model: "S21",
      currentChannelId: 20n,
      memberCount: 0,
      bindings: { activeCount: 0, historicalCount: 0 },
      firmwareConvergence: {
        totalCount: 0,
        pendingCount: 0,
        updatingCount: 0,
        verifyingCount: 0,
        confirmedCount: 0,
        attentionCount: 0,
        members: [],
      },
      channels: [],
      compatibility: "compatible",
    },
  ],
};

const files: FirmwareFileInfo[] = [
  {
    id: "proto-v2",
    filename: "proto-v2.swu",
    size: 1,
    uploaded_at: "2026-08-24T00:00:00Z",
    target_manufacturer: "Proto",
    target_model: "Alpha",
    firmware_version: "2.0.0",
  },
  {
    id: "antminer-v2",
    filename: "antminer-v2.bin",
    size: 1,
    uploaded_at: "2026-08-24T00:00:00Z",
    target_manufacturer: "Antminer",
    target_model: "S21",
    firmware_version: "2.0.0",
  },
  {
    id: "whatsminer-v1",
    filename: "whatsminer-v1.bin",
    size: 1,
    uploaded_at: "2026-08-24T00:00:00Z",
    target_manufacturer: "MicroBT",
    target_model: "M60",
    firmware_version: "1.0.0",
  },
];

describe("ManageRolloutLaneDeclarationsModal", () => {
  it("excludes declared models with miners and adds a zero-member model", async () => {
    const user = userEvent.setup();
    const onPreview = vi.fn().mockResolvedValue({
      laneId: lane.id,
      targets: [],
      miners: [],
      matchingCount: 0,
      mismatchedCount: 0,
      unknownCount: 0,
      reassignments: [],
      requiresReassignmentConfirmation: false,
      reassignmentConfirmationToken: "",
    });
    const onCreate = vi.fn().mockResolvedValue(lane);
    const onUpdated = vi.fn();
    render(
      <ManageRolloutLaneDeclarationsModal
        open
        lane={lane}
        files={files}
        isSubmitting={false}
        onDismiss={vi.fn()}
        onPreview={onPreview}
        onCreate={onCreate}
        onPublish={vi.fn()}
        onUpdated={onUpdated}
      />,
    );

    expect(screen.queryByText("proto-v2.swu")).not.toBeInTheDocument();
    await user.click(screen.getByText("whatsminer-v1.bin"));
    await user.click(screen.getByRole("button", { name: "Review and add model" }));

    await waitFor(() =>
      expect(onCreate).toHaveBeenCalledWith(
        expect.objectContaining({
          laneId: lane.id,
          expectedRevision: 0n,
          firmwareFileId: "whatsminer-v1",
          deviceIdentifiers: [],
        }),
      ),
    );
    expect(onUpdated).toHaveBeenCalledWith(lane, "Added MicroBT M60");
  });

  it("publishes an empty declaration without rollout start input", async () => {
    const user = userEvent.setup();
    const onPublish = vi.fn().mockResolvedValue(lane);
    render(
      <ManageRolloutLaneDeclarationsModal
        open
        lane={lane}
        files={files}
        isSubmitting={false}
        onDismiss={vi.fn()}
        onPreview={vi.fn()}
        onCreate={vi.fn()}
        onPublish={onPublish}
        onUpdated={vi.fn()}
      />,
    );

    await user.click(screen.getByText("antminer-v2.bin"));
    await user.click(screen.getByRole("button", { name: "Publish target" }));

    await waitFor(() =>
      expect(onPublish).toHaveBeenCalledWith(
        expect.objectContaining({
          laneId: lane.id,
          laneModelId: "empty-antminer",
          expectedRevision: 4n,
          firmwareFileId: "antminer-v2",
        }),
      ),
    );
  });

  it("keeps declaration preview failures local", async () => {
    const user = userEvent.setup();
    render(
      <ManageRolloutLaneDeclarationsModal
        open
        lane={lane}
        files={files}
        isSubmitting={false}
        onDismiss={vi.fn()}
        onPreview={vi.fn().mockRejectedValue(new Error("preview unavailable"))}
        onCreate={vi.fn()}
        onPublish={vi.fn()}
        onUpdated={vi.fn()}
      />,
    );

    await user.click(screen.getByText("whatsminer-v1.bin"));
    await user.click(screen.getByRole("button", { name: "Review and add model" }));

    expect(await screen.findByText("Couldn’t preview the model declaration")).toBeInTheDocument();
    expect(screen.getByText("preview unavailable")).toBeInTheDocument();
  });
});
