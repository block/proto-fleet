import type { ComponentProps } from "react";
import { render, screen, waitFor, within } from "@testing-library/react";
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
      <button
        type="button"
        disabled={Boolean(buttons?.[0]?.loading || buttons?.[0]?.disabled)}
        onClick={buttons?.[0]?.onClick}
      >
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
  targetKw: "40",
  restoreBatchSize: "10",
  restoreIntervalSec: "120",
  reason: "Grid peak - ERCOT 4CP signal",
};

const preview: CurtailmentPlanPreview = {
  selectedMinerCount: 18,
  targetKw: 40,
  estimatedReductionKw: 45,
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

function mockVisibleSelectLayout(): () => void {
  const getBoundingClientRectSpy = vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue({
    x: 16,
    y: 16,
    width: 320,
    height: 56,
    top: 16,
    right: 336,
    bottom: 72,
    left: 16,
    toJSON: () => ({}),
  } as DOMRect);

  return () => getBoundingClientRectSpy.mockRestore();
}

describe("CurtailmentStartModal", () => {
  it("renders the empty state and target selectors", () => {
    renderModal();

    expect(screen.getByRole("dialog", { name: "Plan a curtailment" })).toBeInTheDocument();
    expect(screen.getAllByText("Configure your curtailment to see a preview.")).toHaveLength(2);
    expect(screen.getByText("Response profile")).toBeInTheDocument();
    expect(screen.getByText("Custom plan")).toBeInTheDocument();
    expect(screen.getByText("Curtail behavior")).toBeInTheDocument();
    expect(screen.getByText("Fixed kW reduction")).toBeInTheDocument();
    expect(screen.getByText("Least efficient first")).toBeInTheDocument();
    expect(screen.queryByText("Safety")).not.toBeInTheDocument();
    expect(screen.queryByText("Normal")).not.toBeInTheDocument();
    expect(screen.getByText("Restore behavior")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Racks\s+Select/ })).toBeEnabled();
    expect(screen.getByRole("button", { name: /Groups\s+Select/ })).toBeEnabled();
    expect(screen.getByRole("button", { name: /Miners\s+Select/ })).toBeEnabled();
  });

  it("renders preview and preview error states", () => {
    const { rerender } = renderModal({ initialValues: configuredValues, preview });

    expect(screen.getAllByText("Curtail 18 miners across the fleet immediately")).toHaveLength(2);
    expect(screen.getAllByText("Target reduction")).toHaveLength(3);
    expect(screen.getAllByText("45.0 kW of 40.0 kW")).toHaveLength(2);
    expect(screen.queryByText("Estimated time to restore ~2 minutes")).not.toBeInTheDocument();

    const secondaryPane = within(screen.getByTestId("secondary-pane"));
    expect(secondaryPane.queryByText("Time to curtail")).not.toBeInTheDocument();
    expect(secondaryPane.getByText("Time to restore")).toBeInTheDocument();
    expect(secondaryPane.getAllByText("~2 minutes")).toHaveLength(1);

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

  it("renders estimated reduction against the requested reduction", () => {
    renderModal({
      initialValues: {
        ...configuredValues,
        targetKw: "40",
      },
      preview: {
        ...preview,
        targetKw: 40,
        estimatedReductionKw: 48,
      },
    });

    expect(screen.getAllByText("48.0 kW of 40.0 kW")).toHaveLength(2);
  });

  it("submits the current form values without dismissing the modal", async () => {
    const user = userEvent.setup();
    const { onDismiss, onSubmit } = renderModal();
    const targetInput = screen.getByLabelText("Target reduction");
    const restoreBatchSizeInput = screen.getByLabelText("Batch size (miners)");
    const restoreIntervalInput = screen.getByLabelText("Batch interval (sec)");

    await user.type(targetInput, "75");
    await user.type(restoreBatchSizeInput, "10");
    await user.type(restoreIntervalInput, "120");
    await user.type(screen.getByLabelText("Reason"), "Grid response");
    await user.click(screen.getByRole("button", { name: "Start curtailment" }));

    expect(screen.getByText("Force include maintenance miners?")).toBeInTheDocument();
    expect(onSubmit).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Force include" }));

    const submittedValues = onSubmit.mock.calls[0]?.[0] as Record<string, unknown>;
    expect(submittedValues).toMatchObject({
      targetKw: "75",
      toleranceKw: "",
      minDurationSec: "",
      maxDurationSec: "",
      reason: "Grid response",
      priority: "normal",
      responseProfileId: "customPlan",
      curtailmentMode: "fixedKwReduction",
      minerSelectionStrategy: "leastEfficientFirst",
      restoreBatchSize: "10",
      restoreIntervalSec: "120",
      includeMaintenance: true,
    });
    expect(onDismiss).not.toHaveBeenCalled();
  });

  it("only exposes curtailment options supported by the current API", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({ initialValues: { includeMaintenance: false } });
    const startButton = screen.getByRole("button", { name: "Start curtailment" });
    const restoreSelectLayoutMock = mockVisibleSelectLayout();

    expect(startButton).toBeEnabled();

    try {
      await user.click(screen.getByRole("button", { name: "Curtailment mode" }));
      const fixedReductionOption = await screen.findByRole("option", { name: "Fixed kW reduction" });
      expect(screen.queryByRole("option", { name: "Percentage reduction" })).not.toBeInTheDocument();
      await user.click(fixedReductionOption);

      await user.click(screen.getByRole("button", { name: "Miner selection strategy" }));
      const leastEfficientOption = await screen.findByRole("option", { name: "Least efficient first" });
      expect(screen.queryByRole("option", { name: "Round robin" })).not.toBeInTheDocument();
      expect(screen.queryByRole("option", { name: "Oldest miners first" })).not.toBeInTheDocument();
      expect(screen.queryByRole("option", { name: "Lowest hashrate first" })).not.toBeInTheDocument();
      await user.click(leastEfficientOption);
    } finally {
      restoreSelectLayoutMock();
    }

    expect(screen.getByText("Fixed kW reduction")).toBeInTheDocument();
    expect(screen.getByText("Least efficient first")).toBeInTheDocument();
    expect(startButton).toBeEnabled();

    await user.click(startButton);
    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        curtailmentMode: "fixedKwReduction",
        minerSelectionStrategy: "leastEfficientFirst",
      }),
    );
  });

  it("includes maintenance miners by default and confirms re-inclusion", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal();

    expect(getMaintenanceCheckbox()).toBeChecked();
    expect(screen.queryByText("Requires explicit force acknowledgement")).not.toBeInTheDocument();

    await user.click(screen.getByText("Include miners in maintenance"));

    expect(getMaintenanceCheckbox()).not.toBeChecked();
    expect(screen.queryByText("Force include maintenance miners?")).not.toBeInTheDocument();

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
    const { onSubmit } = renderModal({ initialValues: { includeMaintenance: false } });

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

    const targetInput = screen.getByLabelText("Target reduction");
    await user.clear(targetInput);
    await user.type(targetInput, "99");
    expect(targetInput).toHaveValue(99);

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

    const updatedTargetInput = screen.getByLabelText("Target reduction");
    expect(updatedTargetInput).toHaveValue(25);
    expect(screen.getByLabelText("Reason")).toHaveValue("Updated reason");
  });

  it("renders field validation errors with accessible error state", () => {
    renderModal({
      errors: {
        targetKw: "Required",
        reason: "Reason is required",
      },
    });

    const targetInput = screen.getByLabelText("Target reduction");
    expect(targetInput).toHaveAttribute("aria-invalid", "true");
    expect(targetInput).toHaveAttribute("aria-describedby", "curtailment-target-kw-error");
    expect(screen.getByText("Required")).toBeInTheDocument();
    expect(screen.getByLabelText("Reason")).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByText("Reason is required")).toBeInTheDocument();
  });
});
