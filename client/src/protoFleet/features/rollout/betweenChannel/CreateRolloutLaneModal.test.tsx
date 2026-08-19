import type { HTMLAttributes, ReactNode } from "react";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import CreateRolloutLaneModal from "./CreateRolloutLaneModal";
import type { FirmwareFileInfo } from "@/protoFleet/api/useFirmwareApi";

vi.mock("motion/react", () => ({
  AnimatePresence: ({ children }: { children: ReactNode }) => children,
  motion: {
    div: ({ children, ...props }: HTMLAttributes<HTMLDivElement>) => <div {...props}>{children}</div>,
  },
}));

vi.mock("@/protoFleet/features/settings/components/Schedules/MinerSelectionModal", () => ({
  default: ({ open, onSave }: { open: boolean; onSave: (value: { selectedMinerIds: string[] }) => void }) =>
    open ? <button onClick={() => onSave({ selectedMinerIds: ["miner-1", "miner-2"] })}>Use test miners</button> : null,
}));

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
];

describe("CreateRolloutLaneModal", () => {
  it("previews matching miners and creates without confirmation", async () => {
    const user = userEvent.setup();
    const onCreate = vi.fn();
    const onPreview = vi.fn().mockResolvedValue({
      targets: [],
      miners: [],
      matchingCount: 2,
      mismatchedCount: 0,
      unknownCount: 0,
      reassignments: [],
      requiresReassignmentConfirmation: false,
      reassignmentConfirmationToken: "",
    });
    render(
      <CreateRolloutLaneModal
        open
        files={files}
        isSubmitting={false}
        onDismiss={vi.fn()}
        onPreview={onPreview}
        onCreate={onCreate}
      />,
    );

    await user.type(screen.getByLabelText("Lane name"), "Stable production");
    await user.type(screen.getByLabelText("Description"), "Production firmware");
    await user.click(screen.getByText("alpha-1.swu"));
    await user.click(screen.getByRole("button", { name: "Select miners" }));
    await user.click(screen.getByRole("button", { name: "Use test miners" }));
    await user.click(screen.getByRole("button", { name: "Create lane" }));

    expect(onPreview).toHaveBeenCalledWith({
      firmwareFileIds: ["alpha-1"],
      deviceIdentifiers: ["miner-1", "miner-2"],
    });
    expect(onCreate).toHaveBeenCalledWith({
      label: "Stable production",
      description: "Production firmware",
      firmwareFileIds: ["alpha-1"],
      deviceIdentifiers: ["miner-1", "miner-2"],
      confirmInitialEnforcement: false,
      confirmReassignment: false,
    });
  });

  it("warns for mismatched and unknown firmware, cancels without create, then confirms exact payload", async () => {
    const user = userEvent.setup();
    const onCreate = vi.fn();
    const onPreview = vi.fn().mockResolvedValue({
      targets: [],
      miners: [],
      matchingCount: 0,
      mismatchedCount: 1,
      unknownCount: 1,
      reassignments: [],
      requiresReassignmentConfirmation: false,
      reassignmentConfirmationToken: "",
    });
    render(
      <CreateRolloutLaneModal
        open
        files={files}
        isSubmitting={false}
        onDismiss={vi.fn()}
        onPreview={onPreview}
        onCreate={onCreate}
      />,
    );

    await user.type(screen.getByLabelText("Lane name"), "Stable production");
    await user.click(screen.getByText("alpha-1.swu"));
    await user.click(screen.getByRole("button", { name: "Select miners" }));
    await user.click(screen.getByRole("button", { name: "Use test miners" }));
    await user.click(screen.getByRole("button", { name: "Create lane" }));

    expect(await screen.findByText("Review lane creation changes")).toBeInTheDocument();
    expect(screen.getByText(/1 mismatched miner and 1 miner with unknown firmware/i)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onCreate).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Create lane" }));
    await user.click(await screen.findByRole("button", { name: "Confirm and create lane" }));
    expect(onCreate).toHaveBeenCalledWith({
      label: "Stable production",
      description: "",
      firmwareFileIds: ["alpha-1"],
      deviceIdentifiers: ["miner-1", "miner-2"],
      confirmInitialEnforcement: true,
      confirmReassignment: false,
    });
  });

  it("creates an empty lane directly without a firmware preview", async () => {
    const user = userEvent.setup();
    const onCreate = vi.fn();
    const onPreview = vi.fn();
    render(
      <CreateRolloutLaneModal
        open
        files={files}
        isSubmitting={false}
        onDismiss={vi.fn()}
        onPreview={onPreview}
        onCreate={onCreate}
      />,
    );

    await user.type(screen.getByLabelText("Lane name"), "Empty stable");
    await user.click(screen.getByText("alpha-1.swu"));
    expect(screen.getAllByText("0 miners").length).toBeGreaterThan(0);
    await user.click(screen.getByRole("button", { name: "Create lane" }));

    expect(onPreview).not.toHaveBeenCalled();
    expect(onCreate).toHaveBeenCalledWith({
      label: "Empty stable",
      description: "",
      firmwareFileIds: ["alpha-1"],
      deviceIdentifiers: [],
      confirmInitialEnforcement: false,
      confirmReassignment: false,
    });
  });

  it("combines reassignment and firmware warnings before creating", async () => {
    const user = userEvent.setup();
    const onCreate = vi.fn();
    const onPreview = vi.fn().mockResolvedValue({
      targets: [],
      miners: [],
      matchingCount: 0,
      mismatchedCount: 1,
      unknownCount: 0,
      reassignments: [
        {
          deviceIdentifier: "miner-1",
          sourceLaneId: "source-lane",
          sourceLaneLabel: "Canary",
        },
      ],
      requiresReassignmentConfirmation: true,
      reassignmentConfirmationToken: "preview-token",
    });
    render(
      <CreateRolloutLaneModal
        open
        files={files}
        isSubmitting={false}
        onDismiss={vi.fn()}
        onPreview={onPreview}
        onCreate={onCreate}
      />,
    );

    await user.type(screen.getByLabelText("Lane name"), "Stable production");
    await user.click(screen.getByText("alpha-1.swu"));
    await user.click(screen.getByRole("button", { name: "Select miners" }));
    await user.click(screen.getByRole("button", { name: "Use test miners" }));
    await user.click(screen.getByRole("button", { name: "Create lane" }));

    expect(await screen.findByText(/already assigned to Canary/i)).toBeInTheDocument();
    expect(screen.getByText(/1 mismatched miner/i)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Confirm and create lane" }));
    expect(onCreate).toHaveBeenCalledWith(
      expect.objectContaining({
        confirmInitialEnforcement: true,
        confirmReassignment: true,
        reassignmentConfirmationToken: "preview-token",
      }),
    );
  });

  it("renders mutation errors and locks submission while loading", () => {
    render(
      <CreateRolloutLaneModal
        open
        files={files}
        isSubmitting
        error="Firmware artifact is immutable"
        onDismiss={vi.fn()}
        onPreview={vi.fn()}
        onCreate={vi.fn()}
      />,
    );

    expect(screen.getByText("Lane could not be created")).toBeInTheDocument();
    expect(screen.getByText("Firmware artifact is immutable")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Creating..." })).toBeDisabled();
  });
});
