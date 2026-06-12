import type { ComponentProps } from "react";
import type { RenderResult } from "@testing-library/react";
import { render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import type { FullScreenTwoPaneModalProps } from "@/protoFleet/components/FullScreenTwoPaneModal";
import CurtailmentStartModal, {
  type CurtailmentFormValues,
  type CurtailmentPlanPreview,
  type CurtailmentResponseProfileOption,
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
  createCurtailmentPlanPreview: (
    values: CurtailmentFormValues,
    source: { selectedMinerCount: number; targetKw?: number; estimatedReductionKw: number },
  ): CurtailmentPlanPreview => ({
    selectedMinerCount: source.selectedMinerCount,
    targetKw: source.targetKw ?? Number(values.targetKw),
    estimatedReductionKw: source.estimatedReductionKw,
    curtailEstimate: "~1 minute",
    restoreEstimate: "~2 minutes",
    scopeLabel: "across the fleet",
  }),
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
  curtailBatchSize: "8",
  curtailBatchIntervalSec: "30",
  restoreBatchSize: "10",
  restoreIntervalSec: "120",
  reason: "Grid peak - ERCOT 4CP signal",
};

const responseProfiles: CurtailmentResponseProfileOption[] = [
  {
    id: "standard-shed",
    label: "Standard shed",
    values: {
      curtailmentMode: "fixedKwReduction",
      scopeType: "explicitMiners",
      scopeId: undefined,
      siteId: "",
      deviceSetIds: [],
      deviceIdentifiers: ["miner-1", "miner-2", "miner-3"],
      targetKw: "50",
      curtailBatchSize: "20",
      curtailBatchIntervalSec: "60",
      restoreBatchSize: "10",
      restoreIntervalSec: "120",
      includeMaintenance: true,
    },
  },
  {
    id: "emergency-shed",
    label: "Emergency shed",
    values: {
      curtailmentMode: "fullFleet",
      targetKw: "",
      curtailBatchSize: "60",
      curtailBatchIntervalSec: "30",
      restoreBatchSize: "20",
      restoreIntervalSec: "120",
      includeMaintenance: true,
    },
  },
];

const preview: CurtailmentPlanPreview = {
  selectedMinerCount: 18,
  targetKw: 40,
  estimatedReductionKw: 45,
  curtailEstimate: "~1 minute",
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

function getCurtailmentConfirmation(): HTMLElement {
  return screen.getByTestId("curtailment-run-confirmation");
}

async function confirmCurtailment(user: ReturnType<typeof userEvent.setup>, confirmText = "Run curtailment") {
  await user.click(within(getCurtailmentConfirmation()).getByRole("button", { name: confirmText }));
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

    expect(screen.getByRole("dialog", { name: "New curtailment" })).toBeInTheDocument();
    expect(screen.getAllByText("Configure your curtailment to see a preview.")).toHaveLength(2);
    expect(screen.getByText("Response profile")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Profile" })).toHaveTextContent("Custom plan");
    expect(screen.getByLabelText("Reason")).toBeInTheDocument();
    expect(screen.getByText("Curtail behavior")).toBeInTheDocument();
    expect(screen.getByText("Fleet will automatically curtail the least efficient miners first.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Curtailment mode" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "About curtailment mode" })).toBeInTheDocument();
    expect(screen.getByText("Fixed kW reduction")).toBeInTheDocument();
    expect(screen.getByLabelText("Fixed target reduction (kW)")).toBeInTheDocument();
    expect(screen.getByLabelText("Fixed target reduction (kW)")).toHaveAttribute("type", "text");
    expect(screen.getByLabelText("Fixed target reduction (kW)")).toHaveAttribute("inputmode", "decimal");
    expect(screen.queryByRole("button", { name: "Miner selection strategy" })).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Min duration (sec)")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Max duration (sec)")).not.toBeInTheDocument();
    expect(screen.queryByText("Safety")).not.toBeInTheDocument();
    expect(screen.queryByText("Normal")).not.toBeInTheDocument();
    expect(screen.queryByTestId("curtailment-curtail-batch-size")).not.toBeInTheDocument();
    expect(screen.queryByTestId("curtailment-curtail-batch-interval")).not.toBeInTheDocument();
    expect(screen.getByText("Restore behavior")).toBeInTheDocument();
    expect(screen.getAllByLabelText("Batch size (miners)")).toHaveLength(1);
    expect(screen.getAllByLabelText("Batch interval (sec)")).toHaveLength(1);
    expect(screen.queryByRole("button", { name: /Racks\s+Select/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Groups\s+Select/ })).not.toBeInTheDocument();
    expect(
      screen.getByText("Applies to all miners by default. Use the options below to narrow the scope."),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Miners\s+Select/ })).toBeEnabled();
  });

  it("shows curtailment mode help without opening the mode dropdown", async () => {
    const user = userEvent.setup();
    renderModal();

    await user.click(screen.getByRole("button", { name: "About curtailment mode" }));

    expect(screen.getByTestId("curtailment-mode-info-popover")).toBeInTheDocument();
    expect(screen.getByText("How power reduction is measured: fixed kW target or full shutdown.")).toBeInTheDocument();
    expect(screen.queryByText(/percentage/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/miner count/i)).not.toBeInTheDocument();
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
  });

  it("shows field help popovers for start curtailment inputs", async () => {
    const user = userEvent.setup();
    renderModal({ initialValues: { ...configuredValues, includeMaintenance: false } });

    await user.click(screen.getByRole("button", { name: "About fixed target reduction" }));
    expect(screen.getByText("The amount to reduce based on the selected mode.")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "About restore batch size" }));
    expect(screen.getByText("Number of miners to bring back online in each wave.")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "About restore batch interval" }));
    expect(screen.getByText("Seconds to wait between each restore wave.")).toBeInTheDocument();
  });

  it("applies response profile values and switches back to custom plan after edits", async () => {
    const user = userEvent.setup();
    renderModal({ responseProfiles });

    await user.type(screen.getByLabelText("Reason"), "Operator-requested event");
    await user.click(screen.getByRole("button", { name: "Profile" }));
    await user.click(screen.getByText("Standard shed"));

    expect(screen.getByRole("button", { name: "Profile" })).toHaveTextContent("Standard shed");
    expect(screen.getByLabelText("Reason")).toHaveValue("Operator-requested event");
    expect(screen.getByLabelText("Fixed target reduction (kW)")).toHaveValue("50");
    expect(screen.getByRole("button", { name: /Miners\s+Select/ })).toBeInTheDocument();
    expect(screen.queryByLabelText("Min duration (sec)")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Max duration (sec)")).not.toBeInTheDocument();
    expect(screen.queryByTestId("curtailment-curtail-batch-size")).not.toBeInTheDocument();
    expect(screen.queryByTestId("curtailment-curtail-batch-interval")).not.toBeInTheDocument();
    expect(screen.getAllByLabelText("Batch size (miners)")[0]).toHaveValue("10");
    expect(screen.getAllByLabelText("Batch interval (sec)")[0]).toHaveValue("120");

    await user.clear(screen.getByLabelText("Reason"));
    await user.type(screen.getByLabelText("Reason"), "Updated operator reason");

    expect(screen.getByRole("button", { name: "Profile" })).toHaveTextContent("Standard shed");
    expect(screen.getByLabelText("Reason")).toHaveValue("Updated operator reason");

    await user.clear(screen.getByLabelText("Fixed target reduction (kW)"));
    await user.type(screen.getByLabelText("Fixed target reduction (kW)"), "75");

    expect(screen.getByRole("button", { name: "Profile" })).toHaveTextContent("Custom plan");
  });

  it("renders the response profile create variant", async () => {
    const user = userEvent.setup();
    const onTestCurtailment = vi.fn();
    mockUseCurtailmentPlanPreview.mockReturnValue({
      preview,
      previewError: undefined,
      isPreviewLoading: false,
    });
    const { onSubmit } = renderModal({
      variant: "responseProfile",
      initialValues: {
        ...configuredValues,
        scopeType: "site",
        scopeId: "Austin, TX",
        siteId: "101",
        curtailmentMode: "fullFleet",
        targetKw: "",
        includeMaintenance: true,
      },
      onTestCurtailment,
    });

    expect(screen.getByRole("dialog", { name: "Create response profile" })).toBeInTheDocument();
    expect(screen.queryByRole("dialog", { name: "New curtailment" })).not.toBeInTheDocument();
    expect(screen.getByText("Profile")).toBeInTheDocument();
    expect(
      screen.getByText("Saved configurations that define how much power to shed and how to restore it."),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Profile name")).toHaveValue("Grid peak - ERCOT 4CP signal");
    expect(screen.queryByLabelText("Reason")).not.toBeInTheDocument();
    expect(screen.queryByText("No miners match this curtailment.")).not.toBeInTheDocument();
    expect(screen.queryByText("Apply to")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Miners\s+Select/ })).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Min duration (sec)")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Max duration (sec)")).not.toBeInTheDocument();
    expect(screen.getAllByLabelText("Batch size (miners)")).toHaveLength(2);
    expect(screen.getAllByLabelText("Batch interval (sec)")).toHaveLength(2);
    expect(screen.getByTestId("response-profile-curtail-batch-size")).toHaveValue("8");
    expect(screen.getByTestId("response-profile-curtail-batch-interval")).toHaveValue("30");
    expect(screen.getByText("Restore behavior")).toBeInTheDocument();
    expect(screen.getByTestId("response-profile-restore-batch-size")).toHaveValue("10");
    expect(screen.getByTestId("response-profile-restore-batch-interval")).toHaveValue("120");
    expect(mockUseCurtailmentPlanPreview).toHaveBeenCalledWith(expect.objectContaining({ disabled: false }));
    expect(screen.getAllByText("Curtail 18 miners across the fleet immediately")).toHaveLength(2);

    await user.click(screen.getByRole("button", { name: "Run curtailment" }));
    expect(screen.getByText("Force include maintenance miners?")).toBeInTheDocument();
    expect(onTestCurtailment).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "Force include" }));
    expect(screen.getByText("Run curtailment?")).toBeInTheDocument();
    expect(
      screen.getByText(
        "This will save the profile, then trigger curtailment for the whole fleet. Schedules stay suppressed until miners are restored.",
      ),
    ).toBeInTheDocument();
    expect(onTestCurtailment).not.toHaveBeenCalled();
    await confirmCurtailment(user);
    expect(onTestCurtailment).toHaveBeenCalledWith(
      expect.objectContaining({
        reason: "Grid peak - ERCOT 4CP signal",
        siteId: "",
        curtailmentMode: "fullFleet",
        curtailBatchSize: "8",
        curtailBatchIntervalSec: "30",
        restoreBatchSize: "10",
        restoreIntervalSec: "120",
        scopeType: "wholeOrg",
        scopeId: "whole-org",
        deviceSetIds: [],
        deviceIdentifiers: [],
        includeMaintenance: true,
      }),
    );

    await user.click(screen.getByRole("button", { name: "Save profile" }));

    await waitFor(() => expect(screen.queryByText("Force include maintenance miners?")).not.toBeInTheDocument());
    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        reason: "Grid peak - ERCOT 4CP signal",
        siteId: "",
        curtailBatchSize: "8",
        curtailBatchIntervalSec: "30",
        restoreBatchSize: "10",
        restoreIntervalSec: "120",
        scopeType: "wholeOrg",
        scopeId: "whole-org",
        deviceSetIds: [],
        deviceIdentifiers: [],
        includeMaintenance: true,
      }),
    );
  });

  it("keeps response profile actions enabled while preview is loading", () => {
    mockUseCurtailmentPlanPreview.mockReturnValue({
      preview: undefined,
      previewError: undefined,
      isPreviewLoading: true,
    });

    renderModal({
      variant: "responseProfile",
      initialValues: configuredValues,
      onTestCurtailment: vi.fn(),
    });

    expect(screen.getByRole("button", { name: "Run curtailment" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Save profile" })).toBeEnabled();
    expect(screen.getAllByLabelText("Loading curtailment preview")).toHaveLength(2);
  });

  it("normalizes response profile initial scopes inside confirmation sentences", async () => {
    const user = userEvent.setup();
    renderModal({
      variant: "responseProfile",
      initialValues: {
        ...configuredValues,
        scopeType: "site",
        scopeId: "All sites",
        siteId: "101",
        includeMaintenance: false,
      },
      onTestCurtailment: vi.fn(),
    });

    await user.click(screen.getByRole("button", { name: "Run curtailment" }));

    expect(
      screen.getByText(
        "This will save the profile, then trigger curtailment for miners across the fleet. Schedules stay suppressed until miners are restored.",
      ),
    ).toBeInTheDocument();
  });

  it("shows field help popovers for response profile inputs", async () => {
    const user = userEvent.setup();
    renderModal({
      variant: "responseProfile",
      initialValues: {
        ...configuredValues,
        curtailmentMode: "fixedKwReduction",
        targetKw: "500",
        includeMaintenance: false,
      },
    });

    await user.click(screen.getByRole("button", { name: "About curtailment mode" }));
    expect(screen.getByText("How power reduction is measured: fixed kW target or full shutdown.")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "About fixed target reduction" }));
    expect(screen.getByText("The amount to reduce based on the selected mode.")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "About curtail batch size" }));
    expect(screen.getByText("Number of miners to shut down in each wave.")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "About curtail batch interval" }));
    expect(screen.getByText("Seconds to wait between each curtailment wave.")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "About restore batch size" }));
    expect(screen.getByText("Number of miners to bring back online in each wave.")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "About restore batch interval" }));
    expect(screen.getByText("Seconds to wait between each restore wave.")).toBeInTheDocument();
  });

  it("renders the response profile edit variant with prefilled fields and delete action", async () => {
    const user = userEvent.setup();
    const onDeleteResponseProfile = vi.fn();
    const onTestCurtailment = vi.fn();
    const { onSubmit } = renderModal({
      variant: "responseProfile",
      responseProfileMode: "edit",
      onDeleteResponseProfile,
      onTestCurtailment,
      initialValues: {
        ...configuredValues,
        reason: "Site Alpha 500 kW",
        scopeType: "site",
        scopeId: "Denver, CO",
        siteId: "102",
        curtailmentMode: "fixedKwReduction",
        targetKw: "500",
        curtailBatchSize: "50",
        curtailBatchIntervalSec: "30",
        restoreBatchSize: "10000",
        restoreIntervalSec: "0",
        includeMaintenance: false,
      },
    });

    expect(screen.getByRole("dialog", { name: "Edit response profile" })).toBeInTheDocument();
    expect(screen.getByLabelText("Profile name")).toHaveValue("Site Alpha 500 kW");
    expect(screen.getByRole("button", { name: "Curtailment mode" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Curtailment mode" })).toHaveTextContent("Fixed kW reduction");
    expect(screen.getByLabelText("Fixed target reduction (kW)")).toBeEnabled();
    expect(screen.getByLabelText("Fixed target reduction (kW)")).toHaveValue("500");
    expect(screen.queryByLabelText("Min duration (sec)")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Max duration (sec)")).not.toBeInTheDocument();
    expect(screen.getAllByLabelText("Batch size (miners)")).toHaveLength(2);
    expect(screen.getAllByLabelText("Batch interval (sec)")).toHaveLength(2);
    expect(screen.getByTestId("response-profile-curtail-batch-size")).toHaveValue("50");
    expect(screen.getByTestId("response-profile-curtail-batch-interval")).toHaveValue("30");
    expect(screen.getByText("Restore behavior")).toBeInTheDocument();
    expect(screen.getByTestId("response-profile-restore-batch-size")).toHaveValue("10000");
    expect(screen.getByTestId("response-profile-restore-batch-interval")).toHaveValue("0");
    expect(screen.queryByText("Apply to")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Miners\s+Select/ })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Delete" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Run curtailment" })).toBeEnabled();

    await user.clear(screen.getByLabelText("Profile name"));
    await user.type(screen.getByLabelText("Profile name"), "Site Alpha 750 kW");
    await user.clear(screen.getByLabelText("Fixed target reduction (kW)"));
    await user.type(screen.getByLabelText("Fixed target reduction (kW)"), "750");
    await user.click(screen.getByRole("button", { name: "Run curtailment" }));
    expect(screen.getByText("Run curtailment?")).toBeInTheDocument();
    expect(
      screen.getByText(
        "This will save the profile, then trigger curtailment for miners in Denver, CO. Schedules stay suppressed until miners are restored.",
      ),
    ).toBeInTheDocument();
    expect(onTestCurtailment).not.toHaveBeenCalled();
    await confirmCurtailment(user);

    expect(onTestCurtailment).toHaveBeenCalledWith(
      expect.objectContaining({
        reason: "Site Alpha 750 kW",
        siteId: "102",
        scopeType: "site",
        scopeId: "Denver, CO",
        targetKw: "750",
        curtailBatchSize: "50",
        curtailBatchIntervalSec: "30",
        restoreBatchSize: "10000",
        restoreIntervalSec: "0",
      }),
    );

    await user.click(screen.getByRole("button", { name: "Save profile" }));

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        reason: "Site Alpha 750 kW",
        siteId: "102",
        scopeType: "site",
        scopeId: "Denver, CO",
        targetKw: "750",
        curtailBatchSize: "50",
        curtailBatchIntervalSec: "30",
        restoreBatchSize: "10000",
        restoreIntervalSec: "0",
      }),
    );

    await user.click(screen.getByRole("button", { name: "Delete" }));
    expect(onDeleteResponseProfile).toHaveBeenCalledOnce();
  });

  it("renders edit mode with the full plan visible and locked where fields are not updateable", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      mode: "edit",
      initialValues: {
        ...configuredValues,
        includeMaintenance: false,
      },
    });

    expect(screen.getByRole("dialog", { name: "Manage curtailment" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Curtailment mode" })).toBeDisabled();
    expect(screen.getByLabelText("Fixed target reduction (kW)")).toBeDisabled();
    expect(screen.getByRole("button", { name: /Miners\s+Whole fleet/ })).toBeDisabled();
    expect(screen.getByText("Include miners in maintenance").closest("label")).toHaveClass("cursor-not-allowed");

    const saveButton = screen.getByRole("button", { name: "Save" });
    expect(saveButton).toBeDisabled();

    await user.clear(screen.getByLabelText("Batch interval (sec)"));
    await user.type(screen.getByLabelText("Batch interval (sec)"), "180");
    expect(saveButton).toBeEnabled();
    await user.click(saveButton);

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        restoreIntervalSec: "180",
        reason: "Grid peak - ERCOT 4CP signal",
      }),
    );
  });

  it("keeps in-progress edit values when initial values refresh", async () => {
    const user = userEvent.setup();
    const { rerender } = renderModal({
      mode: "edit",
      initialValues: {
        ...configuredValues,
        includeMaintenance: false,
      },
      preview,
    });

    await user.clear(screen.getByLabelText("Reason"));
    await user.type(screen.getByLabelText("Reason"), "Operator draft");

    rerender(
      <CurtailmentStartModal
        open
        mode="edit"
        initialValues={{
          ...configuredValues,
          reason: "Server refresh",
          restoreIntervalSec: "240",
          includeMaintenance: false,
        }}
        onDismiss={vi.fn()}
        onSubmit={vi.fn()}
        preview={preview}
      />,
    );

    expect(screen.getByLabelText("Reason")).toHaveValue("Operator draft");
    expect(screen.getByLabelText("Batch interval (sec)")).toHaveValue("120");
  });

  it("blocks restore interval clears in edit mode", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      mode: "edit",
      initialValues: {
        ...configuredValues,
        includeMaintenance: false,
      },
      preview,
    });
    const saveButton = screen.getByRole("button", { name: "Save" });

    await user.clear(screen.getByLabelText("Batch interval (sec)"));

    expect(screen.getByText("Restore interval cannot be cleared.")).toBeInTheDocument();
    expect(saveButton).toBeDisabled();

    await user.click(saveButton);
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("blocks zero restore interval in edit mode", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      mode: "edit",
      initialValues: {
        ...configuredValues,
        includeMaintenance: false,
      },
      preview,
    });
    const saveButton = screen.getByRole("button", { name: "Save" });

    await user.clear(screen.getByLabelText("Batch interval (sec)"));
    await user.type(screen.getByLabelText("Batch interval (sec)"), "0");

    expect(screen.getByText("Enter batch interval greater than 0.")).toBeInTheDocument();
    expect(saveButton).toBeDisabled();

    await user.click(saveButton);
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("submits edit mode without maintenance confirmation", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      mode: "edit",
      initialValues: {
        ...configuredValues,
        includeMaintenance: true,
      },
      preview,
    });
    const saveButton = screen.getByRole("button", { name: "Save" });

    await user.clear(screen.getByLabelText("Batch interval (sec)"));
    await user.type(screen.getByLabelText("Batch interval (sec)"), "180");
    await user.click(saveButton);

    expect(screen.queryByText("Force include maintenance miners?")).not.toBeInTheDocument();
    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        includeMaintenance: true,
        restoreIntervalSec: "180",
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

    expect(screen.getByRole("dialog", { name: "New curtailment" })).toBeInTheDocument();
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
    expect(screen.getAllByText("~1 minute to curtail, ~2 minutes to restore")).toHaveLength(2);
  });

  it("blocks submission while the API preview reports a blocking error", async () => {
    const user = userEvent.setup();
    mockUseCurtailmentPlanPreview.mockReturnValue({
      preview: undefined,
      previewError: "No miners match this curtailment.",
      isPreviewLoading: false,
    });
    const { onSubmit } = renderModal({ initialValues: { ...configuredValues, includeMaintenance: false } });
    const startButton = screen.getByRole("button", { name: "Run curtailment" });

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

    expect(screen.getAllByText("Curtailment target reduction")).toHaveLength(2);
    expect(screen.getAllByText("Curtail 18 miners across the fleet immediately")).toHaveLength(2);
    expect(screen.getByRole("button", { name: "Run curtailment" })).toBeDisabled();
    expect(screen.queryByLabelText("Loading curtailment preview")).not.toBeInTheDocument();
    expect(screen.queryByText("Configure your curtailment to see a preview.")).not.toBeInTheDocument();
  });

  it("renders preview and preview error states", () => {
    const { rerender } = renderModal({ initialValues: configuredValues, preview });

    expect(screen.getAllByText("Curtailment target reduction")).toHaveLength(2);
    expect(screen.getAllByText("Curtail 18 miners across the fleet immediately")).toHaveLength(2);
    expect(screen.getAllByText("45.0 kW of 40.0 kW")).toHaveLength(2);
    expect(screen.queryByText("Estimated time to restore ~2 minutes")).not.toBeInTheDocument();

    const secondaryPane = within(screen.getByTestId("secondary-pane"));
    expect(secondaryPane.getByText("Curtailment target reduction")).toBeInTheDocument();
    expect(secondaryPane.getByText("~1 minute to curtail, ~2 minutes to restore")).toBeInTheDocument();

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
    expect(screen.getByRole("button", { name: "Run curtailment" })).toBeDisabled();
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
    const targetInput = screen.getByLabelText("Fixed target reduction (kW)");
    const restoreBatchSizeInput = screen.getAllByLabelText("Batch size (miners)")[0];
    const restoreIntervalInput = screen.getAllByLabelText("Batch interval (sec)")[0];

    await user.type(targetInput, "75");
    await user.type(restoreBatchSizeInput, "10");
    await user.type(restoreIntervalInput, "120");
    await user.type(screen.getByLabelText("Reason"), "Grid response");
    await user.click(screen.getByRole("button", { name: "Run curtailment" }));

    expect(screen.getByText("Force include maintenance miners?")).toBeInTheDocument();
    expect(onSubmit).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Force include" }));
    expect(screen.getByText("Run curtailment?")).toBeInTheDocument();
    expect(
      screen.getByText(
        "This will curtail miners across the fleet immediately. Schedules stay suppressed until miners are restored.",
      ),
    ).toBeInTheDocument();
    expect(onSubmit).not.toHaveBeenCalled();
    await confirmCurtailment(user);

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
      curtailBatchSize: "",
      curtailBatchIntervalSec: "",
      restoreBatchSize: "10",
      restoreIntervalSec: "120",
      includeMaintenance: true,
    });
    expect(onDismiss).not.toHaveBeenCalled();
  });

  it("submits full-shutdown curtailment without requiring a target reduction", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      initialValues: {
        restoreBatchSize: "10",
        restoreIntervalSec: "120",
        reason: "Grid response",
        includeMaintenance: false,
      },
    });
    const startButton = screen.getByRole("button", { name: "Run curtailment" });

    expect(startButton).toBeDisabled();

    await user.click(screen.getByRole("button", { name: "Curtailment mode" }));
    const fullShutdownOption = await screen.findByText("Full shutdown");
    expect(document.body.querySelectorAll('input[type="radio"]')).toHaveLength(0);
    await user.click(fullShutdownOption);

    expect(screen.queryByLabelText("Fixed target reduction (kW)")).not.toBeInTheDocument();
    expect(screen.getByText("Fleet will automatically curtail the least efficient miners first.")).toBeInTheDocument();
    expect(startButton).toBeEnabled();

    await user.click(startButton);
    expect(screen.getByText("Run curtailment?")).toBeInTheDocument();
    expect(
      screen.getByText(
        "This will curtail the whole fleet immediately. Schedules stay suppressed until miners are restored.",
      ),
    ).toBeInTheDocument();
    await confirmCurtailment(user);

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        curtailmentMode: "fullFleet",
        targetKw: "",
      }),
    );
  });

  it("renders full-shutdown mode as locked in edit mode", () => {
    renderModal({
      mode: "edit",
      initialValues: {
        ...configuredValues,
        curtailmentMode: "fullFleet",
        targetKw: "",
        includeMaintenance: false,
      },
    });

    expect(screen.getByRole("button", { name: "Curtailment mode" })).toBeDisabled();
    expect(screen.getByText("Full shutdown")).toBeInTheDocument();
    expect(screen.queryByLabelText("Fixed target reduction (kW)")).not.toBeInTheDocument();
  });

  it("submits default curtailment options with the custom plan selected", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({ initialValues: { ...configuredValues, includeMaintenance: false } });
    const startButton = screen.getByRole("button", { name: "Run curtailment" });

    expect(startButton).toBeEnabled();
    expect(screen.getByRole("button", { name: "Profile" })).toHaveTextContent("Custom plan");
    expect(screen.getByRole("button", { name: "Curtailment mode" })).toBeInTheDocument();
    expect(screen.getByText("Fixed kW reduction")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Miner selection strategy" })).not.toBeInTheDocument();
    expect(screen.getByText("Fleet will automatically curtail the least efficient miners first.")).toBeInTheDocument();
    expect(startButton).toBeEnabled();

    await user.click(startButton);
    expect(screen.getByText("Run curtailment?")).toBeInTheDocument();
    expect(
      screen.getByText(
        "This will curtail miners across the fleet immediately. Schedules stay suppressed until miners are restored.",
      ),
    ).toBeInTheDocument();
    await user.click(within(getCurtailmentConfirmation()).getByRole("button", { name: "Cancel" }));
    expect(onSubmit).not.toHaveBeenCalled();

    await user.click(startButton);
    await confirmCurtailment(user);
    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        curtailmentMode: "fixedKwReduction",
        minerSelectionStrategy: "leastEfficientFirst",
      }),
    );
  });

  it("includes maintenance miners by default and confirms re-inclusion", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({ initialValues: configuredValues });

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

    await user.click(screen.getByRole("button", { name: "Run curtailment" }));
    expect(screen.getByText("Run curtailment?")).toBeInTheDocument();
    await confirmCurtailment(user);
    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ includeMaintenance: true }));
  });

  it("opens target selectors and submits the selected target scope", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({ initialValues: { ...configuredValues, includeMaintenance: false } });
    const startButton = screen.getByRole("button", { name: "Run curtailment" });

    expect(screen.queryByRole("button", { name: /Racks\s+Select/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Groups\s+Select/ })).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /Miners\s+Select/ }));
    await user.click(screen.getByRole("button", { name: "Save miners" }));
    expect(screen.getByRole("button", { name: /Miners\s+3 miners/ })).toBeInTheDocument();
    expect(startButton).toBeEnabled();

    await user.click(startButton);
    expect(screen.getByText("Run curtailment?")).toBeInTheDocument();
    expect(
      screen.getByText("This will curtail 3 miners immediately. Schedules stay suppressed until miners are restored."),
    ).toBeInTheDocument();
    await confirmCurtailment(user);
    expect(onSubmit).toHaveBeenLastCalledWith(
      expect.objectContaining({
        scopeType: "explicitMiners",
        scopeId: undefined,
        deviceSetIds: [],
        deviceIdentifiers: ["miner-1", "miner-2", "miner-3"],
      }),
    );
  });

  it("blocks invalid uint32-backed numeric settings", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      variant: "responseProfile",
      initialValues: {
        ...configuredValues,
        includeMaintenance: false,
      },
    });
    const saveButton = screen.getByRole("button", { name: "Save profile" });
    const batchSizeInput = screen.getByTestId("response-profile-curtail-batch-size");
    const batchIntervalInput = screen.getByTestId("response-profile-curtail-batch-interval");

    await user.clear(batchSizeInput);
    await user.type(batchSizeInput, "10001");
    await user.clear(batchIntervalInput);
    await user.type(batchIntervalInput, "1.5");

    expect(screen.getByText("Enter batch size of 10,000 or less.")).toBeInTheDocument();
    expect(screen.getByText("Enter batch interval as a whole number.")).toBeInTheDocument();
    expect(batchSizeInput).toHaveAttribute("aria-invalid", "true");
    expect(batchIntervalInput).toHaveAttribute("aria-invalid", "true");
    expect(saveButton).toBeDisabled();

    await user.click(saveButton);
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

    const targetInput = screen.getByLabelText("Fixed target reduction (kW)");
    await user.clear(targetInput);
    await user.type(targetInput, "99");
    expect(targetInput).toHaveValue("99");

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

    const updatedTargetInput = screen.getByLabelText("Fixed target reduction (kW)");
    expect(updatedTargetInput).toHaveValue("25");
    expect(screen.getByLabelText("Reason")).toHaveValue("Updated reason");
  });

  it("blocks submissions when parent field errors are present", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      initialValues: { ...configuredValues, includeMaintenance: false },
      errors: {
        targetKw: "Required",
      },
    });
    const startButton = screen.getByRole("button", { name: "Run curtailment" });

    expect(screen.getByText("Required")).toBeInTheDocument();
    expect(startButton).toBeDisabled();

    await user.click(startButton);
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("keeps required start-field validation hidden until fields are edited", async () => {
    const user = userEvent.setup();
    renderModal();

    const targetInput = screen.getByLabelText("Fixed target reduction (kW)");
    const reasonInput = screen.getByLabelText("Reason");

    expect(screen.queryByText("Enter a target reduction.")).not.toBeInTheDocument();
    expect(screen.queryByText("Enter a reason.")).not.toBeInTheDocument();

    await user.type(reasonInput, " ");
    await user.type(targetInput, "5");
    await user.clear(targetInput);

    expect(reasonInput).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByText("Enter a reason.")).toBeInTheDocument();
    expect(targetInput).toHaveAttribute("aria-invalid", "true");
    expect(targetInput).toHaveAttribute("aria-describedby", "curtailment-target-kw-error");
    expect(screen.getByText("Enter a target reduction.")).toBeInTheDocument();
  });
});
