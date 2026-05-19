import type { ComponentProps } from "react";
import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import type { FullScreenTwoPaneModalProps } from "@/protoFleet/components/FullScreenTwoPaneModal";
import CurtailmentStartModal, {
  type CurtailmentFormValues,
  type CurtailmentPlanPreview,
} from "@/protoFleet/features/energy/CurtailmentStartModal";

type MockFullScreenTwoPaneModalProps = Pick<
  FullScreenTwoPaneModalProps,
  "title" | "isBusy" | "buttons" | "abovePanes" | "primaryPane" | "secondaryPane"
>;

vi.mock("@/protoFleet/components/FullScreenTwoPaneModal", () => ({
  default: ({ title, isBusy, buttons, abovePanes, primaryPane, secondaryPane }: MockFullScreenTwoPaneModalProps) => (
    <div role="dialog" aria-label={title} data-busy={isBusy ? "true" : "false"}>
      <button type="button" disabled={Boolean(buttons?.[0]?.loading)} onClick={buttons?.[0]?.onClick}>
        {buttons?.[0]?.text}
      </button>
      <div data-testid="above-panes">{abovePanes}</div>
      <div data-testid="primary-pane">{primaryPane}</div>
      <div data-testid="secondary-pane">{secondaryPane}</div>
    </div>
  ),
}));

vi.mock("@/protoFleet/features/settings/components/Schedules/RackSelectionModal", () => ({
  default: ({ open, onSave }: { open: boolean; onSave: (rackIds: string[]) => void }) =>
    open ? (
      <div role="dialog" aria-label="Rack selection">
        <button type="button" onClick={() => onSave(["rack-1", "rack-2"])}>
          Save racks
        </button>
      </div>
    ) : null,
}));

vi.mock("@/protoFleet/features/settings/components/Schedules/GroupSelectionModal", () => ({
  default: ({ open, onSave }: { open: boolean; onSave: (groupIds: string[]) => void }) =>
    open ? (
      <div role="dialog" aria-label="Group selection">
        <button type="button" onClick={() => onSave(["group-1"])}>
          Save groups
        </button>
      </div>
    ) : null,
}));

vi.mock("@/protoFleet/features/settings/components/Schedules/MinerSelectionModal", () => ({
  default: ({ open, onSave }: { open: boolean; onSave: (minerIds: string[]) => void }) =>
    open ? (
      <div role="dialog" aria-label="Miner selection">
        <button type="button" onClick={() => onSave(["miner-1", "miner-2", "miner-3"])}>
          Save miners
        </button>
      </div>
    ) : null,
}));

const configuredValues: Partial<CurtailmentFormValues> = {
  targetKw: "60",
  minDurationSec: "300",
  maxDurationSec: "3600",
  restoreBatchSize: "10",
  restoreIntervalSec: "120",
  reason: "Grid peak - ERCOT 4CP signal",
};

const preview: CurtailmentPlanPreview = {
  selectedMinerCount: 18,
  targetKw: 60,
  estimatedReductionKw: 60.2,
  restoreEstimate: "~2 minutes",
  scopeLabel: "across the fleet",
};

const renderModal = (props: Partial<ComponentProps<typeof CurtailmentStartModal>> = {}) => {
  const onDismiss = vi.fn();
  const onSubmit = vi.fn();

  return {
    onDismiss,
    onSubmit,
    ...render(<CurtailmentStartModal open onDismiss={onDismiss} onSubmit={onSubmit} {...props} />),
  };
};

const getMaintenanceCheckbox = (): HTMLInputElement => {
  const checkbox = screen.getByText("Include miners in maintenance").closest("label")?.querySelector("input");
  if (!checkbox) {
    throw new Error("Maintenance checkbox was not rendered");
  }
  return checkbox;
};

describe("CurtailmentStartModal", () => {
  it("renders the empty state and target selectors", () => {
    renderModal();

    expect(screen.getByRole("dialog", { name: "Plan a curtailment" })).toBeInTheDocument();
    expect(screen.getAllByText("Configure your curtailment to see a preview.")).toHaveLength(2);
    expect(screen.getByRole("button", { name: /Racks\s+Select/ })).toBeEnabled();
    expect(screen.getByRole("button", { name: /Groups\s+Select/ })).toBeEnabled();
    expect(screen.getByRole("button", { name: /Miners\s+Select/ })).toBeEnabled();
  });

  it("renders preview and preview error states", () => {
    const { rerender } = renderModal({ initialValues: configuredValues, preview });

    expect(screen.getAllByText("Curtail 18 miners across the fleet immediately")).toHaveLength(2);
    expect(screen.getAllByText("60.2 kW of 60.0 kW")).toHaveLength(2);
    expect(screen.getAllByText("~2 minutes")).toHaveLength(2);
    expect(screen.getByText("Estimated time to restore ~2 minutes")).toBeInTheDocument();

    rerender(
      <CurtailmentStartModal
        open
        onDismiss={vi.fn()}
        onSubmit={vi.fn()}
        initialValues={configuredValues}
        previewError="Preview is unavailable until a valid target reduction is entered."
      />,
    );

    expect(screen.getAllByText("Preview is unavailable until a valid target reduction is entered.")).toHaveLength(2);
  });

  it("submits the current form values without dismissing the modal", async () => {
    const user = userEvent.setup();
    const { onDismiss, onSubmit } = renderModal();

    await user.type(screen.getByLabelText("Target reduction"), "75");
    await user.type(screen.getByLabelText("Reason"), "Grid response");
    await user.click(screen.getByRole("button", { name: "Start curtailment" }));

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        targetKw: "75",
        reason: "Grid response",
        priority: "normal",
      }),
    );
    expect(onDismiss).not.toHaveBeenCalled();
  });

  it("requires confirmation before including maintenance miners", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal();

    await user.click(screen.getByText("Include miners in maintenance"));

    expect(screen.getByText("Force include maintenance miners?")).toBeInTheDocument();
    expect(
      screen.getByText("This will run Curtail on miners that are currently flagged for maintenance work."),
    ).toBeInTheDocument();
    expect(getMaintenanceCheckbox()).not.toBeChecked();

    await user.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByText("Force include maintenance miners?")).not.toBeInTheDocument());
    expect(getMaintenanceCheckbox()).not.toBeChecked();

    await user.click(screen.getByText("Include miners in maintenance"));
    await user.click(screen.getByRole("button", { name: "Force include" }));
    await waitFor(() => expect(screen.queryByText("Force include maintenance miners?")).not.toBeInTheDocument());
    expect(getMaintenanceCheckbox()).toBeChecked();

    await user.click(screen.getByRole("button", { name: "Start curtailment" }));
    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ includeMaintenance: true }));
  });

  it("opens target selectors and submits the selected target scope", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal();

    await user.click(screen.getByRole("button", { name: /Racks\s+Select/ }));
    expect(screen.getByRole("dialog", { name: "Rack selection" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Save racks" }));
    expect(screen.getByRole("button", { name: /Racks\s+2 racks/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Start curtailment" }));
    expect(onSubmit).toHaveBeenLastCalledWith(
      expect.objectContaining({
        scopeType: "deviceSet",
        scopeId: "racks",
        deviceSetIds: ["rack-1", "rack-2"],
        deviceIdentifiers: [],
      }),
    );

    await user.click(screen.getByRole("button", { name: /Groups\s+Select/ }));
    await user.click(screen.getByRole("button", { name: "Save groups" }));
    expect(screen.getByRole("button", { name: /Groups\s+1 group/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Start curtailment" }));
    expect(onSubmit).toHaveBeenLastCalledWith(
      expect.objectContaining({
        scopeType: "deviceSet",
        scopeId: "groups",
        deviceSetIds: ["group-1"],
        deviceIdentifiers: [],
      }),
    );

    await user.click(screen.getByRole("button", { name: /Miners\s+Select/ }));
    await user.click(screen.getByRole("button", { name: "Save miners" }));
    expect(screen.getByRole("button", { name: /Miners\s+3 miners/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Start curtailment" }));
    expect(onSubmit).toHaveBeenLastCalledWith(
      expect.objectContaining({
        scopeType: "explicitMiners",
        scopeId: undefined,
        deviceSetIds: [],
        deviceIdentifiers: ["miner-1", "miner-2", "miner-3"],
      }),
    );
  });

  it("resets form values when reopened with new initial values", async () => {
    const user = userEvent.setup();
    const onDismiss = vi.fn();
    const onSubmit = vi.fn();
    const { rerender } = render(
      <CurtailmentStartModal
        open
        onDismiss={onDismiss}
        onSubmit={onSubmit}
        initialValues={{ targetKw: "10", reason: "Initial reason" }}
      />,
    );

    await user.clear(screen.getByLabelText("Target reduction"));
    await user.type(screen.getByLabelText("Target reduction"), "99");
    expect(screen.getByLabelText("Target reduction")).toHaveValue(99);

    rerender(
      <CurtailmentStartModal
        open={false}
        onDismiss={onDismiss}
        onSubmit={onSubmit}
        initialValues={{ targetKw: "10", reason: "Initial reason" }}
      />,
    );
    rerender(
      <CurtailmentStartModal
        open
        onDismiss={onDismiss}
        onSubmit={onSubmit}
        initialValues={{ targetKw: "25", reason: "Updated reason" }}
      />,
    );

    expect(screen.getByLabelText("Target reduction")).toHaveValue(25);
    expect(screen.getByLabelText("Reason")).toHaveValue("Updated reason");
  });

  it("renders field validation errors with accessible error state", () => {
    renderModal({
      errors: {
        targetKw: "Required",
        reason: "Reason is required",
      },
    });

    expect(screen.getByLabelText("Target reduction")).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByLabelText("Target reduction")).toHaveAttribute(
      "aria-describedby",
      "curtailment-target-kw-error",
    );
    expect(screen.getByText("Required")).toBeInTheDocument();
    expect(screen.getByLabelText("Reason")).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByText("Reason is required")).toBeInTheDocument();
  });
});
