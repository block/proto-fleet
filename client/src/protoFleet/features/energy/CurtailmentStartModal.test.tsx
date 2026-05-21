import type { ComponentProps } from "react";
import type { RenderResult } from "@testing-library/react";
import { render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
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

interface RenderModalResult extends RenderResult {
  onDismiss: ReturnType<typeof vi.fn>;
  onSubmit: ReturnType<typeof vi.fn>;
}

const { mockUseCurtailmentPlanPreview } = vi.hoisted(() => ({
  mockUseCurtailmentPlanPreview: vi.fn(),
}));

vi.mock("@/protoFleet/features/energy/useCurtailmentPlanPreview", () => ({
  getUnsupportedDeviceSetPreviewError: (values: CurtailmentFormValues) =>
    values.scopeType === "deviceSet" && values.deviceSetIds.length > 0
      ? "Rack and group curtailment previews are not supported yet. Select specific miners or the whole fleet to preview and start this curtailment."
      : undefined,
  useCurtailmentPlanPreview: mockUseCurtailmentPlanPreview,
}));

vi.mock("@/protoFleet/components/FullScreenTwoPaneModal", () => ({
  default: ({ title, isBusy, buttons, abovePanes, primaryPane, secondaryPane }: MockFullScreenTwoPaneModalProps) => (
    <div role="dialog" aria-label={title} data-busy={isBusy ? "true" : "false"}>
      {buttons?.map((button) => (
        <button
          key={button.text}
          type="button"
          disabled={Boolean(button.loading || button.disabled)}
          onClick={button.onClick}
        >
          {button.text}
        </button>
      ))}
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
  curtailEstimate: "5 minutes - 30 minutes",
  restoreEstimate: "~2 minutes",
  scopeLabel: "across the fleet",
};

function renderModal(props: Partial<ComponentProps<typeof CurtailmentStartModal>> = {}): RenderModalResult {
  const onDismiss = vi.fn();
  const onSubmit = vi.fn();

  return {
    onDismiss,
    onSubmit,
    ...render(<CurtailmentStartModal open onDismiss={onDismiss} onSubmit={onSubmit} {...props} />),
  };
}

function getMaintenanceCheckbox(): HTMLInputElement {
  const checkbox = screen.getByText("Include miners in maintenance").closest("label")?.querySelector("input");
  if (!checkbox) {
    throw new Error("Maintenance checkbox was not rendered");
  }
  return checkbox;
}

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
  beforeEach(() => {
    mockUseCurtailmentPlanPreview.mockReturnValue({
      preview: undefined,
      previewError: undefined,
      isPreviewLoading: false,
    });
  });

  it("renders the empty state and target selectors", () => {
    renderModal();

    expect(screen.getByRole("dialog", { name: "Plan a curtailment" })).toBeInTheDocument();
    expect(screen.getAllByText("Configure your curtailment to see a preview.")).toHaveLength(2);
    expect(screen.getByText("Response profile")).toBeInTheDocument();
    expect(screen.getByText("Custom plan")).toBeInTheDocument();
    expect(screen.getByText("Curtail behavior")).toBeInTheDocument();
    expect(screen.getByText("Fixed kW reduction")).toBeInTheDocument();
    expect(screen.getByText("Least efficient first")).toBeInTheDocument();
    expect(screen.getByLabelText("Min duration (sec)")).toBeInTheDocument();
    expect(screen.getByLabelText("Max duration (sec)")).toBeInTheDocument();
    expect(screen.queryByText("Safety")).not.toBeInTheDocument();
    expect(screen.queryByText("Normal")).not.toBeInTheDocument();
    expect(screen.getByText("Restore behavior")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Racks\s+Select/ })).toBeEnabled();
    expect(screen.getByRole("button", { name: /Groups\s+Select/ })).toBeEnabled();
    expect(screen.getByRole("button", { name: /Miners\s+Select/ })).toBeEnabled();
  });

  it("renders edit mode with prefilled values and save copy", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      mode: "edit",
      initialValues: {
        ...configuredValues,
        includeMaintenance: false,
      },
      preview,
    });

    expect(screen.getByRole("dialog", { name: "Manage curtailment" })).toBeInTheDocument();
    expect(screen.queryByRole("dialog", { name: "Plan a curtailment" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Start curtailment" })).not.toBeInTheDocument();
    expect(screen.getByLabelText("Target reduction")).toHaveValue(40);
    expect(screen.getByLabelText("Reason")).toHaveValue("Grid peak - ERCOT 4CP signal");

    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        targetKw: "40",
        restoreBatchSize: "10",
        restoreIntervalSec: "120",
        reason: "Grid peak - ERCOT 4CP signal",
      }),
    );
  });

  it("renders a stop curtailment action in edit mode", async () => {
    const user = userEvent.setup();
    const onStopCurtailment = vi.fn();
    const { onSubmit } = renderModal({
      mode: "edit",
      initialValues: {
        ...configuredValues,
        includeMaintenance: false,
      },
      onStopCurtailment,
      preview,
    });

    expect(screen.getByRole("dialog", { name: "Manage curtailment" })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Stop curtailment" }));

    expect(onStopCurtailment).toHaveBeenCalledOnce();
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("does not render the stop curtailment action while planning", () => {
    renderModal({
      onStopCurtailment: vi.fn(),
    });

    expect(screen.getByRole("dialog", { name: "Plan a curtailment" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Stop curtailment" })).not.toBeInTheDocument();
  });

  it("renders the API preview when preview props are not controlled", () => {
    mockUseCurtailmentPlanPreview.mockReturnValue({
      preview,
      previewError: undefined,
      isPreviewLoading: false,
    });

    renderModal({ initialValues: configuredValues });

    expect(screen.getAllByText("Curtail 18 miners across the fleet immediately")).toHaveLength(2);
    expect(screen.getAllByText("45.0 kW of 40.0 kW")).toHaveLength(2);
    expect(screen.getAllByText("5 minutes - 30 minutes")).toHaveLength(2);
  });

  it("blocks submission while the API preview reports a blocking error", async () => {
    const user = userEvent.setup();
    mockUseCurtailmentPlanPreview.mockReturnValue({
      preview: undefined,
      previewError: "No miners match this curtailment.",
      isPreviewLoading: false,
    });
    const { onSubmit } = renderModal({ initialValues: { ...configuredValues, includeMaintenance: false } });
    const startButton = screen.getByRole("button", { name: "Start curtailment" });

    expect(screen.getAllByText("No miners match this curtailment.")).toHaveLength(2);
    expect(startButton).toBeDisabled();

    await user.click(startButton);
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("keeps the existing preview visible while a refreshed preview is loading", () => {
    mockUseCurtailmentPlanPreview.mockReturnValue({
      preview,
      previewError: undefined,
      isPreviewLoading: true,
    });

    renderModal({ initialValues: configuredValues });

    expect(screen.getAllByText("Curtail 18 miners across the fleet immediately")).toHaveLength(2);
    expect(screen.queryByLabelText("Loading curtailment preview")).not.toBeInTheDocument();
    expect(screen.queryByText("Configure your curtailment to see a preview.")).not.toBeInTheDocument();
  });

  it("renders preview and preview error states", () => {
    const { rerender } = renderModal({ initialValues: configuredValues, preview });

    expect(screen.getAllByText("Curtail 18 miners across the fleet immediately")).toHaveLength(2);
    expect(screen.getAllByText("Target reduction")).toHaveLength(3);
    expect(screen.getAllByText("45.0 kW of 40.0 kW")).toHaveLength(2);
    expect(screen.queryByText("Estimated time to restore ~2 minutes")).not.toBeInTheDocument();

    const secondaryPane = within(screen.getByTestId("secondary-pane"));
    expect(secondaryPane.getByText("Curtailment duration")).toBeInTheDocument();
    expect(secondaryPane.getByText("5 minutes - 30 minutes")).toBeInTheDocument();
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

  it("shows unsupported target-scope errors before controlled preview props", () => {
    renderModal({
      initialValues: {
        ...configuredValues,
        includeMaintenance: false,
        scopeType: "deviceSet",
        scopeId: "racks",
        deviceSetIds: ["rack-1"],
      },
      preview,
    });

    expect(
      screen.getAllByText(
        "Rack and group curtailment previews are not supported yet. Select specific miners or the whole fleet to preview and start this curtailment.",
      ),
    ).toHaveLength(2);
    expect(screen.queryByText("Curtail 18 miners across the fleet immediately")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Start curtailment" })).toBeDisabled();
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
    const minDurationInput = screen.getByLabelText("Min duration (sec)");
    const maxDurationInput = screen.getByLabelText("Max duration (sec)");
    const restoreBatchSizeInput = screen.getByLabelText("Batch size (miners)");
    const restoreIntervalInput = screen.getByLabelText("Batch interval (sec)");

    await user.type(targetInput, "75");
    await user.type(minDurationInput, "300");
    await user.type(maxDurationInput, "1800");
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
      minDurationSec: "300",
      maxDurationSec: "1800",
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
    const startButton = screen.getByRole("button", { name: "Start curtailment" });

    await user.click(screen.getByRole("button", { name: /Racks\s+Select/ }));
    expect(screen.getByRole("dialog", { name: "Rack selection" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Save racks" }));
    expect(screen.getByRole("button", { name: /Racks\s+2 racks/ })).toBeInTheDocument();
    expect(
      screen.getAllByText(
        "Rack and group curtailment previews are not supported yet. Select specific miners or the whole fleet to preview and start this curtailment.",
      ),
    ).toHaveLength(2);
    expect(startButton).toBeDisabled();

    await user.click(startButton);
    expect(onSubmit).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: /Groups\s+Select/ }));
    await user.click(screen.getByRole("button", { name: "Save groups" }));
    expect(screen.getByRole("button", { name: /Groups\s+1 group/ })).toBeInTheDocument();
    expect(startButton).toBeDisabled();

    await user.click(screen.getByRole("button", { name: /Miners\s+Select/ }));
    await user.click(screen.getByRole("button", { name: "Save miners" }));
    expect(screen.getByRole("button", { name: /Miners\s+3 miners/ })).toBeInTheDocument();
    expect(startButton).toBeEnabled();

    await user.click(startButton);
    expect(onSubmit).toHaveBeenLastCalledWith(
      expect.objectContaining({
        scopeType: "explicitMiners",
        scopeId: undefined,
        deviceSetIds: [],
        deviceIdentifiers: ["miner-1", "miner-2", "miner-3"],
      }),
    );
  });

  it("blocks inverted duration submissions", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      initialValues: {
        includeMaintenance: false,
        minDurationSec: "3600",
        maxDurationSec: "300",
      },
      errors: {
        maxDurationSec: "Server-side max duration error",
      },
    });
    const startButton = screen.getByRole("button", { name: "Start curtailment" });

    expect(screen.getByText("Max duration must be greater than or equal to min duration.")).toBeInTheDocument();
    expect(screen.queryByText("Server-side max duration error")).not.toBeInTheDocument();
    expect(screen.getByLabelText("Max duration (sec)")).toHaveAttribute("aria-invalid", "true");
    expect(startButton).toBeDisabled();

    await user.click(startButton);
    expect(onSubmit).not.toHaveBeenCalled();
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

  it("blocks submissions when parent field errors are present", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      initialValues: { includeMaintenance: false },
      errors: {
        targetKw: "Required",
      },
    });
    const startButton = screen.getByRole("button", { name: "Start curtailment" });

    expect(screen.getByText("Required")).toBeInTheDocument();
    expect(startButton).toBeDisabled();

    await user.click(startButton);
    expect(onSubmit).not.toHaveBeenCalled();
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
