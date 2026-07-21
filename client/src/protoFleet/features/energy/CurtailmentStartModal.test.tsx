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
  type CurtailmentSiteOption,
} from "@/protoFleet/features/energy/CurtailmentStartModal";
import type { FacilityFanDeviceOption } from "@/protoFleet/features/energy/FacilityFanSelectionModal";

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
    source: {
      selectedMinerCount: number;
      facilityFanDeviceCount?: number;
      targetKw?: number;
      estimatedReductionKw: number;
    },
  ): CurtailmentPlanPreview => ({
    selectedMinerCount: source.selectedMinerCount,
    facilityFanDeviceCount: source.facilityFanDeviceCount ?? values.facilityFanDeviceIds?.length ?? 0,
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
  default: ({
    open,
    onSave,
  }: {
    open: boolean;
    onSave: (selection: {
      selectedMinerIds: string[];
      allSelected: boolean;
      totalMiners?: number;
      filter?: { models: string[]; rackIds: bigint[]; groupIds: bigint[] };
    }) => void;
  }) =>
    open ? (
      <div role="dialog" aria-label="Miner selection">
        <button
          type="button"
          onClick={() =>
            onSave({ selectedMinerIds: ["miner-1", "miner-2", "miner-3"], allSelected: false, totalMiners: 3 })
          }
        >
          Save miners
        </button>
        <button
          type="button"
          onClick={() => onSave({ selectedMinerIds: ["miner-1", "miner-2"], allSelected: true, totalMiners: 5000 })}
        >
          Save all miners
        </button>
        <button
          type="button"
          onClick={() =>
            onSave({
              selectedMinerIds: ["miner-1", "miner-2"],
              allSelected: true,
              totalMiners: 2,
              filter: { models: ["S21"], rackIds: [], groupIds: [] },
            })
          }
        >
          Save filtered all miners
        </button>
      </div>
    ) : null,
}));

const configuredValues: Partial<CurtailmentFormValues> = {
  // A configured fixed-kW plan; the modal itself defaults to full shutdown.
  curtailmentMode: "fixedKwReduction",
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

const wholeFleetResponseProfiles: CurtailmentResponseProfileOption[] = [
  {
    id: "standard-shed",
    label: "Standard shed",
    values: {
      ...responseProfiles[0].values,
      scopeType: "wholeOrg",
      scopeId: "whole-org",
      siteId: "",
      deviceSetIds: [],
      deviceIdentifiers: [],
      includeMaintenance: false,
    },
  },
];

const siteResponseProfiles: CurtailmentResponseProfileOption[] = [
  {
    id: "austin-shed",
    label: "Austin site shed",
    values: {
      ...responseProfiles[0].values,
      scopeType: "site",
      scopeId: "Austin, TX",
      siteId: "101",
      deviceSetIds: [],
      deviceIdentifiers: [],
      includeMaintenance: false,
    },
  },
];

const siteOptions: CurtailmentSiteOption[] = [
  { id: "101", name: "Austin, TX" },
  { id: "102", name: "Denver, CO" },
];

const infrastructureDevices: FacilityFanDeviceOption[] = [
  {
    id: "31",
    siteId: "101",
    siteName: "Austin, TX",
    buildingName: "Building 1",
    name: "Fan Unit 1",
    deviceKind: "single_fan",
    fanCount: 1,
    enabled: true,
  },
  {
    id: "32",
    siteId: "101",
    siteName: "Austin, TX",
    buildingName: "Building 2",
    name: "Exhaust Fan Group",
    deviceKind: "fan_group",
    fanCount: 4,
    enabled: false,
  },
  {
    id: "33",
    siteId: "102",
    siteName: "Denver, CO",
    buildingName: "Building 1",
    name: "Denver Fan",
    deviceKind: "single_fan",
    fanCount: 1,
    enabled: true,
  },
];

const scopeLessResponseProfiles: CurtailmentResponseProfileOption[] = [
  {
    ...responseProfiles[1],
    values: {
      ...responseProfiles[1].values,
      includeMaintenance: false,
    },
  },
];

const preview: CurtailmentPlanPreview = {
  selectedMinerCount: 18,
  facilityFanDeviceCount: 0,
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

function getAllPairedCheckbox(): HTMLInputElement {
  const checkbox = screen.getByText("Target all paired miners").closest("label")?.querySelector("input");
  if (!checkbox) {
    throw new Error("All paired miners checkbox was not rendered");
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
    // Full shutdown is the default mode, so no fixed-target input renders.
    expect(screen.getByText("Full shutdown")).toBeInTheDocument();
    expect(screen.queryByLabelText("Fixed target reduction (kW)")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Miner selection strategy" })).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Min duration (sec)")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Max duration (sec)")).not.toBeInTheDocument();
    expect(screen.queryByText("Safety")).not.toBeInTheDocument();
    expect(screen.queryByText("Normal")).not.toBeInTheDocument();
    expect(screen.getByTestId("curtailment-curtail-batch-size")).toBeInTheDocument();
    expect(screen.getByTestId("curtailment-curtail-batch-interval")).toBeInTheDocument();
    expect(screen.getByText("Restore behavior")).toBeInTheDocument();
    expect(screen.getAllByLabelText("Batch size (miners)")).toHaveLength(2);
    expect(screen.getAllByLabelText("Batch interval (sec)")).toHaveLength(2);
    expect(screen.queryByTestId("curtailment-post-event-cooldown")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Racks\s+Select/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Groups\s+Select/ })).not.toBeInTheDocument();
    expect(
      screen.getByText("Choose the sites, miners, and infrastructure included in this curtailment."),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Miners\s+Select/ })).toBeEnabled();
    expect(screen.getByRole("button", { name: /Sites\s+Select/ })).toBeEnabled();
    expect(screen.getByRole("button", { name: /Infrastructure\s+Select/ })).toBeEnabled();
  });

  it("prefills new custom curtailments with the default site scope", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      initialValues: { ...configuredValues, includeMaintenance: false },
      siteOptions,
      defaultSiteScope: siteOptions[0],
    });

    expect(screen.getByRole("button", { name: /Sites\s+Austin, TX/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Run curtailment" }));
    expect(
      screen.getByText(
        "This will curtail miners in Austin, TX immediately. Schedules stay suppressed until miners are restored.",
      ),
    ).toBeInTheDocument();
    await confirmCurtailment(user);

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        responseProfileId: "customPlan",
        scopeType: "site",
        scopeId: "Austin, TX",
        siteSelection: "site",
        siteId: "101",
        siteIds: ["101"],
        siteNamesById: { "101": "Austin, TX" },
        deviceSetIds: [],
        deviceIdentifiers: [],
      }),
    );
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

    await user.click(screen.getByRole("button", { name: "About curtail batch size" }));
    expect(screen.getByText("Number of miners to shut down in each wave.")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "About curtail batch interval" }));
    expect(screen.getByText("Seconds to wait between each curtailment wave.")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "About restore batch size" }));
    expect(
      screen.getByText(
        "Number of miners to bring back online in each wave. 0 or blank restores pending miners up to the safety limit.",
      ),
    ).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "About restore batch interval" }));
    expect(
      screen.getByText("Seconds to wait between each restore wave. 0 or blank means no wait."),
    ).toBeInTheDocument();
  });

  it("applies response profile values and switches back to custom plan after edits", async () => {
    const user = userEvent.setup();
    const customResponseProfiles: CurtailmentResponseProfileOption[] = responseProfiles.map((profile) => ({
      ...profile,
      values: { ...profile.values, includeMaintenance: false },
    }));
    const { onSubmit } = renderModal({ responseProfiles: customResponseProfiles });

    await user.type(screen.getByLabelText("Reason"), "Operator-requested event");
    await user.click(screen.getByRole("button", { name: "Profile" }));
    await user.click(screen.getByText("Standard shed"));

    expect(screen.getByRole("button", { name: "Profile" })).toHaveTextContent("Standard shed");
    expect(screen.getByLabelText("Reason")).toHaveValue("Operator-requested event");
    expect(screen.getByLabelText("Fixed target reduction (kW)")).toHaveValue("50");
    expect(screen.getByRole("button", { name: /Miners\s+3 miners/ })).toBeInTheDocument();
    expect(screen.queryByLabelText("Min duration (sec)")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Max duration (sec)")).not.toBeInTheDocument();
    expect(screen.getByTestId("curtailment-curtail-batch-size")).toHaveValue("20");
    expect(screen.getByTestId("curtailment-curtail-batch-interval")).toHaveValue("60");
    expect(screen.getAllByLabelText("Batch size (miners)")[1]).toHaveValue("10");
    expect(screen.getAllByLabelText("Batch interval (sec)")[1]).toHaveValue("120");
    expect(screen.queryByTestId("curtailment-post-event-cooldown")).not.toBeInTheDocument();

    await user.clear(screen.getByLabelText("Reason"));
    await user.type(screen.getByLabelText("Reason"), "Updated operator reason");

    expect(screen.getByRole("button", { name: "Profile" })).toHaveTextContent("Standard shed");
    expect(screen.getByLabelText("Reason")).toHaveValue("Updated operator reason");

    await user.clear(screen.getByLabelText("Fixed target reduction (kW)"));
    await user.type(screen.getByLabelText("Fixed target reduction (kW)"), "75");

    expect(screen.getByRole("button", { name: "Profile" })).toHaveTextContent("Custom plan");

    await user.click(screen.getByRole("button", { name: "Run curtailment" }));
    expect(screen.getByText("Run curtailment?")).toBeInTheDocument();
    await confirmCurtailment(user);

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        responseProfileId: "customPlan",
        curtailBatchSize: "20",
        curtailBatchIntervalSec: "60",
      }),
    );
  });

  it("preserves visible response profile controls when custom plan is selected", async () => {
    const user = userEvent.setup();
    const customResponseProfiles: CurtailmentResponseProfileOption[] = responseProfiles.map((profile) => ({
      ...profile,
      values: { ...profile.values, includeMaintenance: false },
    }));
    const { onSubmit } = renderModal({ responseProfiles: customResponseProfiles });

    await user.type(screen.getByLabelText("Reason"), "Operator-requested event");
    await user.click(screen.getByRole("button", { name: "Profile" }));
    await user.click(screen.getByText("Standard shed"));
    expect(screen.getByRole("button", { name: "Profile" })).toHaveTextContent("Standard shed");

    await user.click(screen.getByRole("button", { name: "Profile" }));
    await user.click(screen.getByText("Custom plan"));
    expect(screen.getByRole("button", { name: "Profile" })).toHaveTextContent("Custom plan");
    expect(screen.getByTestId("curtailment-curtail-batch-size")).toHaveValue("20");
    expect(screen.getByTestId("curtailment-curtail-batch-interval")).toHaveValue("60");
    expect(screen.queryByTestId("curtailment-post-event-cooldown")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Run curtailment" }));
    expect(screen.getByText("Run curtailment?")).toBeInTheDocument();
    await confirmCurtailment(user);

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        curtailBatchSize: "20",
        curtailBatchIntervalSec: "60",
      }),
    );
  });

  it("restores the selected response profile scope after a target selection", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      initialValues: { ...configuredValues, includeMaintenance: false },
      responseProfiles: wholeFleetResponseProfiles,
    });

    await user.click(screen.getByRole("button", { name: /Miners\s+Select/ }));
    await user.click(screen.getByRole("button", { name: "Save miners" }));
    expect(screen.getByRole("button", { name: /Miners\s+3 miners/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Profile" }));
    await user.click(screen.getByText("Standard shed"));

    expect(screen.getByRole("button", { name: "Profile" })).toHaveTextContent("Standard shed");
    expect(screen.getByRole("button", { name: /Miners\s+Select/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Run curtailment" }));
    expect(screen.getByText("Run curtailment?")).toBeInTheDocument();
    expect(
      screen.getByText(
        "This will curtail miners across the fleet immediately. Schedules stay suppressed until miners are restored.",
      ),
    ).toBeInTheDocument();
    await confirmCurtailment(user);

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        responseProfileId: "standard-shed",
        scopeType: "wholeOrg",
        scopeId: "whole-org",
        siteId: "",
        deviceSetIds: [],
        deviceIdentifiers: [],
      }),
    );
  });

  it("preserves site scope from response profiles in live curtailment create mode", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      initialValues: { ...configuredValues, includeMaintenance: false },
      responseProfiles: siteResponseProfiles,
    });

    await user.click(screen.getByRole("button", { name: /Miners\s+Select/ }));
    await user.click(screen.getByRole("button", { name: "Save miners" }));
    expect(screen.getByRole("button", { name: /Miners\s+3 miners/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Profile" }));
    await user.click(screen.getByText("Austin site shed"));

    expect(screen.getByRole("button", { name: "Profile" })).toHaveTextContent("Austin site shed");
    expect(screen.getByRole("button", { name: /Sites\s+Austin, TX/ })).toBeInTheDocument();

    await user.clear(screen.getByLabelText("Fixed target reduction (kW)"));
    await user.type(screen.getByLabelText("Fixed target reduction (kW)"), "75");

    expect(screen.getByRole("button", { name: "Profile" })).toHaveTextContent("Custom plan");
    expect(screen.getByRole("button", { name: /Sites\s+Austin, TX/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Run curtailment" }));
    expect(screen.getByText("Run curtailment?")).toBeInTheDocument();
    expect(
      screen.getByText(
        "This will curtail miners in Austin, TX immediately. Schedules stay suppressed until miners are restored.",
      ),
    ).toBeInTheDocument();
    await confirmCurtailment(user);

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        responseProfileId: "customPlan",
        scopeType: "site",
        scopeId: "Austin, TX",
        siteId: "101",
        deviceSetIds: [],
        deviceIdentifiers: [],
      }),
    );
  });

  it("allows selecting infrastructure fans in live curtailment create mode", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      initialValues: { ...configuredValues, includeMaintenance: false },
      infrastructureDevices,
    });

    await user.click(screen.getByRole("button", { name: /Infrastructure\s+Select/ }));

    expect(screen.getByText("Fan behavior during curtailment")).toBeInTheDocument();
    await user.click(screen.getByRole("checkbox", { name: /Fan Unit 1/ }));
    await user.clear(screen.getByTestId("facility-fan-off-delay"));
    await user.type(screen.getByTestId("facility-fan-off-delay"), "45");
    await user.clear(screen.getByTestId("facility-fan-restore-delay"));
    await user.type(screen.getByTestId("facility-fan-restore-delay"), "90");
    await user.click(screen.getByRole("button", { name: "Apply" }));

    expect(screen.queryByText("Fan behavior during curtailment")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Infrastructure\s+1 device/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Run curtailment" }));
    expect(screen.getByText("Run curtailment?")).toBeInTheDocument();
    expect(
      screen.getByText(
        "This will curtail miners across the fleet and 1 fan immediately. Schedules stay suppressed until miners are restored.",
      ),
    ).toBeInTheDocument();
    await confirmCurtailment(user);

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        facilityFanDeviceIds: ["31"],
        fanOffDelaySec: "45",
        fanRestoreDelaySec: "90",
      }),
    );
  });

  it("keeps response-profile infrastructure fan settings in live curtailment create mode", async () => {
    const user = userEvent.setup();
    const fanResponseProfiles: CurtailmentResponseProfileOption[] = [
      {
        id: "fan-shed",
        label: "Fan shed",
        values: {
          ...responseProfiles[0].values,
          facilityFanDeviceIds: ["31"],
          fanOffDelaySec: "60",
          fanRestoreDelaySec: "120",
          includeMaintenance: false,
        },
      },
    ];
    const { onSubmit } = renderModal({
      initialValues: { ...configuredValues, includeMaintenance: false },
      responseProfiles: fanResponseProfiles,
    });

    await user.click(screen.getByRole("button", { name: "Profile" }));
    await user.click(screen.getByText("Fan shed"));

    expect(screen.getByRole("button", { name: "Profile" })).toHaveTextContent("Fan shed");
    expect(screen.getByRole("button", { name: /Infrastructure\s+1 device/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Run curtailment" }));
    expect(screen.getByText("Run curtailment?")).toBeInTheDocument();
    expect(
      screen.getByText(
        "This will curtail 3 miners and 1 fan immediately. Schedules stay suppressed until miners are restored.",
      ),
    ).toBeInTheDocument();
    await confirmCurtailment(user);

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        responseProfileId: "fan-shed",
        facilityFanDeviceIds: ["31"],
        fanOffDelaySec: "60",
        fanRestoreDelaySec: "120",
      }),
    );
  });

  it("normalizes a site-scoped response profile without a site id to whole fleet", async () => {
    const user = userEvent.setup();
    const malformedSiteResponseProfiles: CurtailmentResponseProfileOption[] = [
      {
        id: "missing-site-id-shed",
        label: "Missing site id shed",
        values: {
          ...responseProfiles[0].values,
          scopeType: "site",
          scopeId: "Missing site",
          siteId: "   ",
          deviceSetIds: [],
          deviceIdentifiers: [],
          includeMaintenance: false,
        },
      },
    ];
    const { onSubmit } = renderModal({
      initialValues: { ...configuredValues, includeMaintenance: false },
      responseProfiles: malformedSiteResponseProfiles,
    });

    await user.click(screen.getByRole("button", { name: "Profile" }));
    await user.click(screen.getByText("Missing site id shed"));

    expect(screen.getByRole("button", { name: "Profile" })).toHaveTextContent("Missing site id shed");
    expect(screen.getByRole("button", { name: /Miners\s+Select/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Run curtailment" }));
    expect(screen.getByText("Run curtailment?")).toBeInTheDocument();
    await confirmCurtailment(user);

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        responseProfileId: "missing-site-id-shed",
        scopeType: "wholeOrg",
        scopeId: "whole-org",
        siteId: "",
        deviceSetIds: [],
        deviceIdentifiers: [],
      }),
    );
  });

  it("normalizes a site-scoped response profile with an invalid site id to whole fleet", async () => {
    const user = userEvent.setup();
    const malformedSiteResponseProfiles: CurtailmentResponseProfileOption[] = [
      {
        id: "invalid-site-id-shed",
        label: "Invalid site id shed",
        values: {
          ...responseProfiles[0].values,
          scopeType: "site",
          scopeId: "Austin, TX",
          siteId: "austin-tx",
          deviceSetIds: [],
          deviceIdentifiers: [],
          includeMaintenance: false,
        },
      },
    ];
    const { onSubmit } = renderModal({
      initialValues: { ...configuredValues, includeMaintenance: false },
      responseProfiles: malformedSiteResponseProfiles,
    });

    await user.click(screen.getByRole("button", { name: "Profile" }));
    await user.click(screen.getByText("Invalid site id shed"));

    expect(screen.getByRole("button", { name: "Profile" })).toHaveTextContent("Invalid site id shed");
    expect(screen.getByRole("button", { name: /Miners\s+Select/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Run curtailment" }));
    expect(screen.getByText("Run curtailment?")).toBeInTheDocument();
    await confirmCurtailment(user);

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        responseProfileId: "invalid-site-id-shed",
        scopeType: "wholeOrg",
        scopeId: "whole-org",
        siteId: "",
        deviceSetIds: [],
        deviceIdentifiers: [],
      }),
    );
  });

  it("preserves the selected target when a response profile option has no scope values", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      initialValues: { ...configuredValues, includeMaintenance: false },
      responseProfiles: scopeLessResponseProfiles,
    });

    await user.click(screen.getByRole("button", { name: /Miners\s+Select/ }));
    await user.click(screen.getByRole("button", { name: "Save miners" }));
    expect(screen.getByRole("button", { name: /Miners\s+3 miners/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Profile" }));
    await user.click(screen.getByText("Emergency shed"));

    expect(screen.getByRole("button", { name: "Profile" })).toHaveTextContent("Emergency shed");
    expect(screen.getByRole("button", { name: /Miners\s+3 miners/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Run curtailment" }));
    expect(screen.getByText("Run curtailment?")).toBeInTheDocument();
    expect(
      screen.getByText("This will curtail 3 miners immediately. Schedules stay suppressed until miners are restored."),
    ).toBeInTheDocument();
    await confirmCurtailment(user);

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        responseProfileId: "emergency-shed",
        scopeType: "explicitMiners",
        scopeId: undefined,
        deviceSetIds: [],
        deviceIdentifiers: ["miner-1", "miner-2", "miner-3"],
      }),
    );
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
      siteOptions,
      infrastructureDevices,
      initialValues: {
        ...configuredValues,
        scopeType: "site",
        scopeId: "Stale Austin label",
        siteId: "101",
        curtailmentMode: "fullFleet",
        targetKw: "",
        facilityFanDeviceIds: ["31", "32"],
        fanOffDelaySec: "45",
        fanRestoreDelaySec: "90",
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
    expect(screen.getByText("Apply to")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Miners\s+Select/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Sites\s+Austin, TX/ })).toBeInTheDocument();
    expect(screen.queryByLabelText("Min duration (sec)")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Max duration (sec)")).not.toBeInTheDocument();
    expect(screen.getAllByLabelText("Batch size (miners)")).toHaveLength(2);
    expect(screen.getAllByLabelText("Batch interval (sec)")).toHaveLength(2);
    expect(screen.getByTestId("response-profile-curtail-batch-size")).toHaveValue("8");
    expect(screen.getByTestId("response-profile-curtail-batch-interval")).toHaveValue("30");
    expect(screen.getByText("Restore behavior")).toBeInTheDocument();
    expect(screen.getByTestId("response-profile-restore-batch-size")).toHaveValue("10");
    expect(screen.getByTestId("response-profile-restore-batch-interval")).toHaveValue("120");
    expect(screen.queryByText("Facility fans")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Infrastructure\s+2 devices/ })).toBeInTheDocument();
    expect(screen.queryByTestId("response-profile-post-event-cooldown")).not.toBeInTheDocument();
    expect(mockUseCurtailmentPlanPreview).toHaveBeenCalledWith(expect.objectContaining({ disabled: false }));
    expect(screen.getAllByText("Curtail 18 miners across the fleet immediately")).toHaveLength(2);

    await user.click(screen.getByRole("button", { name: /Infrastructure\s+2 devices/ }));
    expect(screen.getByText("Fan behavior during curtailment")).toBeInTheDocument();
    expect(screen.getByText("3 devices in scope")).toBeInTheDocument();
    expect(screen.getByTestId("facility-fan-off-delay")).toHaveValue("45");
    expect(screen.getByTestId("facility-fan-restore-delay")).toHaveValue("90");
    expect(screen.getByText("Exhaust Fan Group")).toBeInTheDocument();
    expect(screen.getByText("Disabled")).toBeInTheDocument();
    expect(screen.getByText("Denver Fan")).toBeInTheDocument();

    const disabledFanCheckbox = screen.getByRole("checkbox", { name: /Exhaust Fan Group/ });
    expect(disabledFanCheckbox).toBeDisabled();
    expect(disabledFanCheckbox).toBeChecked();
    await user.click(disabledFanCheckbox);
    expect(disabledFanCheckbox).toBeChecked();

    const fanModalSurface = screen.getByTestId("facility-fan-selection-modal").parentElement;
    if (!fanModalSurface) {
      throw new Error("Fan modal surface was not rendered");
    }
    await user.click(within(fanModalSurface).getByRole("button", { name: "Select all" }));
    expect(disabledFanCheckbox).toBeChecked();
    await user.click(within(fanModalSurface).getByRole("button", { name: "Select none" }));
    await user.click(screen.getByRole("checkbox", { name: /Fan Unit 1/ }));
    await user.clear(screen.getByTestId("facility-fan-off-delay"));
    await user.type(screen.getByTestId("facility-fan-off-delay"), "60");
    await user.clear(screen.getByTestId("facility-fan-restore-delay"));
    await user.type(screen.getByTestId("facility-fan-restore-delay"), "120");
    await user.click(screen.getByRole("button", { name: "Apply" }));

    expect(screen.queryByText("Fan behavior during curtailment")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Infrastructure\s+1 device/ })).toBeInTheDocument();

    expect(screen.getByRole("button", { name: "Run curtailment" })).toBeEnabled();
    expect(
      screen.getByText("Choose the sites, miners, and infrastructure included in this curtailment."),
    ).toBeVisible();

    await user.click(screen.getByRole("button", { name: "Save profile" }));

    await waitFor(() => expect(screen.queryByText("Force include maintenance miners?")).not.toBeInTheDocument());
    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        reason: "Grid peak - ERCOT 4CP signal",
        siteId: "101",
        curtailBatchSize: "8",
        curtailBatchIntervalSec: "30",
        restoreBatchSize: "10",
        restoreIntervalSec: "120",
        facilityFanDeviceIds: ["31"],
        fanOffDelaySec: "60",
        fanRestoreDelaySec: "120",
        scopeType: "site",
        scopeId: "Austin, TX",
        deviceSetIds: [],
        deviceIdentifiers: [],
        includeMaintenance: true,
      }),
    );
  });

  it("selects site scope when creating a response profile", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      variant: "responseProfile",
      initialValues: {
        ...configuredValues,
        includeMaintenance: false,
      },
      siteOptions: [...siteOptions, { id: "103", name: "Calgary" }],
    });

    await user.click(screen.getByRole("button", { name: /Sites\s+Select/ }));
    expect(screen.getByText("Select sites")).toBeInTheDocument();
    expect(screen.queryByText("All miners in the fleet")).not.toBeInTheDocument();
    await user.click(screen.getByTestId("response-profile-scope-site-102"));
    await user.click(screen.getByRole("button", { name: "Done" }));

    await user.click(screen.getByRole("button", { name: "Save profile" }));

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        siteId: "102",
        siteIds: ["102"],
        scopeType: "site",
        scopeId: "Denver, CO",
        deviceSetIds: [],
        deviceIdentifiers: [],
      }),
    );
  });

  it("preserves selected infrastructure devices when the site scope changes", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      variant: "responseProfile",
      siteOptions,
      infrastructureDevices,
      initialValues: {
        ...configuredValues,
        scopeType: "site",
        scopeId: "Austin, TX",
        siteSelection: "site",
        siteId: "101",
        siteIds: ["101"],
        facilityFanDeviceIds: ["31"],
      },
    });

    expect(screen.getByRole("button", { name: /Infrastructure\s+1 device/ })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /Sites\s+Austin, TX/ }));
    await user.click(screen.getByTestId("response-profile-scope-site-101"));
    await user.click(screen.getByTestId("response-profile-scope-site-102"));
    await user.click(screen.getByRole("button", { name: "Done" }));

    expect(screen.getByRole("button", { name: /Infrastructure\s+1 device/ })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Save profile" }));
    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ facilityFanDeviceIds: ["31"] }));
  });

  it("allows infrastructure devices outside the selected sites", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      variant: "responseProfile",
      siteOptions,
      infrastructureDevices,
      initialValues: {
        ...configuredValues,
        scopeType: "site",
        scopeId: "Austin, TX",
        siteSelection: "site",
        siteId: "101",
        siteIds: ["101"],
        facilityFanDeviceIds: [],
      },
    });

    await user.click(screen.getByRole("button", { name: /Infrastructure\s+Select/ }));
    await user.click(screen.getByRole("checkbox", { name: /Denver Fan/ }));
    await user.click(screen.getByRole("button", { name: "Apply" }));
    await user.click(screen.getByRole("button", { name: "Save profile" }));

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        siteIds: ["101"],
        facilityFanDeviceIds: ["33"],
      }),
    );
  });

  it("disables infrastructure fan selection for an automation-bound profile", async () => {
    const user = userEvent.setup();
    const disabledReason =
      "An automation uses this profile. Update or delete the automation before changing infrastructure fans.";
    renderModal({
      variant: "responseProfile",
      responseProfileMode: "edit",
      initialValues: configuredValues,
      infrastructureDevices,
      facilityFanSelectionDisabledReason: disabledReason,
    });

    const infrastructureButton = screen.getByRole("button", { name: /Infrastructure\s+Select/ });
    expect(infrastructureButton).toBeDisabled();
    expect(screen.getByText(disabledReason)).toBeInTheDocument();

    await user.click(infrastructureButton);
    expect(screen.queryByText("Fan behavior during curtailment")).not.toBeInTheDocument();
  });

  it.each([
    { name: "loading", isLoadingInfrastructureDevices: true, infrastructureDevicesError: null },
    {
      name: "a load failure",
      isLoadingInfrastructureDevices: false,
      infrastructureDevicesError: "Failed to load infrastructure devices.",
    },
  ])(
    "disables fan settings Apply during $name",
    async ({ isLoadingInfrastructureDevices, infrastructureDevicesError }) => {
      const user = userEvent.setup();
      renderModal({
        variant: "responseProfile",
        infrastructureDevices: [],
        initialValues: {
          ...configuredValues,
          facilityFanDeviceIds: ["31"],
        },
        isLoadingInfrastructureDevices,
        infrastructureDevicesError,
      });

      await user.click(screen.getByRole("button", { name: /Infrastructure\s+1 device/ }));

      expect(screen.getByRole("button", { name: "Apply" })).toBeDisabled();
    },
  );

  it("prefills response profile creation with the default site scope", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      variant: "responseProfile",
      initialValues: {
        ...configuredValues,
        includeMaintenance: false,
      },
      siteOptions,
      defaultSiteScope: siteOptions[1],
    });

    expect(screen.getByRole("button", { name: /Sites\s+Denver, CO/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Save profile" }));

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        siteSelection: "site",
        siteId: "102",
        siteIds: ["102"],
        siteNamesById: { "102": "Denver, CO" },
        scopeType: "site",
        scopeId: "Denver, CO",
        deviceSetIds: [],
        deviceIdentifiers: [],
      }),
    );
  });

  it("keeps a single selected selectable site as all-sites scope", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      variant: "responseProfile",
      initialValues: {
        ...configuredValues,
        includeMaintenance: false,
      },
      siteOptions: [{ id: "101", name: "Toronto" }],
    });

    await user.click(screen.getByRole("button", { name: /Sites\s+Select/ }));
    await user.click(screen.getByTestId("response-profile-scope-site-101"));
    expect(screen.getByText("All 1 site selected")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Done" }));

    expect(screen.getByRole("button", { name: /Sites\s+All sites/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Save profile" }));

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        siteSelection: "allSites",
        siteId: "101",
        siteIds: ["101"],
        siteNamesById: { "101": "Toronto" },
        scopeType: "site",
        scopeId: "All sites",
        deviceSetIds: [],
        deviceIdentifiers: [],
      }),
    );
  });

  it("selects multiple site scopes before saving a response profile", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      variant: "responseProfile",
      initialValues: {
        ...configuredValues,
        includeMaintenance: false,
      },
      siteOptions: [...siteOptions, { id: "103", name: "Calgary" }],
    });

    await user.click(screen.getByRole("button", { name: /Sites\s+Select/ }));
    await user.click(screen.getByTestId("response-profile-scope-site-101"));
    await user.click(screen.getByTestId("response-profile-scope-site-102"));
    await user.click(screen.getByRole("button", { name: "Done" }));

    expect(screen.getByRole("button", { name: /Sites\s+2 sites/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Save profile" }));

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        siteId: "101",
        siteIds: ["101", "102"],
        siteNamesById: { "101": "Austin, TX", "102": "Denver, CO" },
        scopeType: "site",
        scopeId: "2 sites",
        deviceSetIds: [],
        deviceIdentifiers: [],
      }),
    );
  });

  it("disables site scope when it is unavailable for the current user", async () => {
    renderModal({
      variant: "responseProfile",
      initialValues: configuredValues,
      siteScopeEnabled: false,
      siteScopeDisabledReason: "Site scope is not available for the current user.",
    });

    const user = userEvent.setup();

    await user.click(screen.getByRole("button", { name: /Sites\s+Select/ }));

    expect(within(screen.getByTestId("response-profile-scope-site-unavailable")).getByRole("checkbox")).toBeDisabled();
    expect(screen.getByTestId("response-profile-scope-site-unavailable")).toHaveTextContent(
      "Site scope is not available for the current user.",
    );
  });

  it("preserves hidden site scope when site scope is disabled", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      variant: "responseProfile",
      siteScopeEnabled: false,
      initialValues: {
        ...configuredValues,
        scopeType: "site",
        scopeId: "Austin, TX",
        siteId: "101",
        includeMaintenance: false,
      },
    });

    expect(screen.getByRole("button", { name: /Sites\s+Austin, TX/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /Sites\s+Austin, TX/ }));

    expect(within(screen.getByTestId("response-profile-scope-site-101")).getByRole("checkbox")).toBeDisabled();
    expect(within(screen.getByTestId("response-profile-scope-site-101")).getByRole("checkbox")).toBeChecked();

    await user.click(screen.getByRole("button", { name: "Done" }));

    await user.click(screen.getByRole("button", { name: "Save profile" }));

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        siteId: "101",
        siteIds: ["101"],
        scopeType: "site",
        scopeId: "Austin, TX",
      }),
    );
  });

  it("clears site selection without clearing selected miners", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      variant: "responseProfile",
      siteOptions,
      initialValues: {
        ...configuredValues,
        scopeType: "explicitMiners",
        scopeId: "Austin, TX",
        siteSelection: "site",
        siteId: "101",
        deviceIdentifiers: ["miner-1", "miner-2"],
        includeMaintenance: false,
      },
    });

    expect(screen.getByRole("button", { name: /Miners\s+2 miners/ })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /Sites\s+Austin, TX/ }));
    await user.click(screen.getByRole("button", { name: "Select none" }));
    await user.click(screen.getByRole("button", { name: "Done" }));

    expect(screen.getByRole("button", { name: /Sites\s+Select/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Miners\s+2 miners/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Save profile" }));

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        scopeType: "explicitMiners",
        scopeId: undefined,
        siteSelection: "none",
        siteId: "",
        siteIds: [],
        deviceIdentifiers: ["miner-1", "miner-2"],
      }),
    );
  });

  it("renders a missing saved site as a disabled fallback row", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      variant: "responseProfile",
      siteOptions: [{ id: "102", name: "Denver, CO" }],
      initialValues: {
        ...configuredValues,
        scopeType: "site",
        scopeId: "Austin, TX",
        siteId: "101",
        includeMaintenance: false,
      },
    });

    await user.click(screen.getByRole("button", { name: /Sites\s+Austin, TX/ }));

    const savedSiteRow = screen.getByTestId("response-profile-scope-site-101");
    expect(within(savedSiteRow).getByRole("checkbox")).toBeDisabled();
    expect(savedSiteRow).toHaveTextContent("Austin, TX");
    expect(savedSiteRow).not.toHaveTextContent("Saved site");
    expect(within(savedSiteRow).getByRole("checkbox")).toBeChecked();
    expect(within(screen.getByTestId("response-profile-scope-site-102")).getByRole("checkbox")).toBeEnabled();

    await user.click(screen.getByRole("button", { name: "Done" }));
    await user.click(screen.getByRole("button", { name: "Save profile" }));

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        siteId: "101",
        siteIds: ["101"],
        scopeType: "site",
        scopeId: "Austin, TX",
      }),
    );
  });

  it("normalizes an invalid response profile initial site id before saving", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      variant: "responseProfile",
      initialValues: {
        ...configuredValues,
        scopeType: "site",
        scopeId: "Austin, TX",
        siteId: "austin-tx",
        includeMaintenance: false,
      },
    });

    expect(screen.getByRole("button", { name: /Sites\s+Select/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /Sites\s+Select/ }));

    expect(screen.queryByTestId("response-profile-scope-site-austin-tx")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Save profile" }));

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        siteId: "",
        siteIds: [],
        scopeType: "wholeOrg",
        scopeId: "whole-org",
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

  it("allows saving a response profile when live preview has no current curtailable load", async () => {
    const user = userEvent.setup();
    const previewError =
      "insufficient curtailable load: 0.000 kW available, 0.000 kW requested, tolerance 0.000 kW, candidate_min_power_w=1500W; excluded: power_telemetry_unreliable=4, pairing=4, active_event=6";
    mockUseCurtailmentPlanPreview.mockReturnValue({
      preview: undefined,
      previewError,
      isPreviewLoading: false,
    });
    const { onSubmit } = renderModal({
      variant: "responseProfile",
      initialValues: configuredValues,
      onTestCurtailment: vi.fn(),
    });

    expect(screen.queryByText(previewError)).not.toBeInTheDocument();
    expect(screen.getAllByText("Current fleet state is unavailable for preview.")).toHaveLength(2);
    expect(screen.getByRole("button", { name: "Run curtailment" })).toBeDisabled();

    const saveButton = screen.getByRole("button", { name: "Save profile" });
    expect(saveButton).toBeEnabled();

    await user.click(saveButton);

    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ reason: configuredValues.reason }));
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
        "This will save the profile, then trigger curtailment for miners in all sites. Schedules stay suppressed until miners are restored.",
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
    expect(
      screen.getByText(
        "Number of miners to bring back online in each wave. 0 or blank restores pending miners up to the safety limit.",
      ),
    ).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "About restore batch interval" }));
    expect(
      screen.getByText("Seconds to wait between each restore wave. 0 or blank means no wait."),
    ).toBeInTheDocument();
  });

  it("renders the response profile edit variant with prefilled fields and delete action", async () => {
    const user = userEvent.setup();
    const onDeleteResponseProfile = vi.fn();
    const onTestCurtailment = vi.fn();
    const { onSubmit } = renderModal({
      variant: "responseProfile",
      responseProfileMode: "edit",
      siteOptions,
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
        restoreBatchSize: "0",
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
    expect(screen.getByTestId("response-profile-restore-batch-size")).toHaveValue("0");
    expect(screen.getByTestId("response-profile-restore-batch-interval")).toHaveValue("0");
    expect(screen.queryByTestId("response-profile-post-event-cooldown")).not.toBeInTheDocument();
    expect(screen.getByText("Apply to")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Miners\s+Select/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Sites\s+Denver, CO/ })).toBeInTheDocument();
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
        restoreBatchSize: "0",
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
        restoreBatchSize: "0",
        restoreIntervalSec: "0",
      }),
    );

    await user.click(screen.getByRole("button", { name: "Delete" }));
    expect(onDeleteResponseProfile).toHaveBeenCalledOnce();
  });

  it("keeps all selectable sites as an all-sites site scope", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      variant: "responseProfile",
      responseProfileMode: "edit",
      siteOptions,
      initialValues: {
        ...configuredValues,
        reason: "Site Alpha 500 kW",
        scopeType: "site",
        scopeId: "Austin, TX",
        siteId: "101",
        includeMaintenance: false,
      },
    });

    await user.click(screen.getByRole("button", { name: /Sites\s+Austin, TX/ }));
    await user.click(screen.getByRole("button", { name: "Select all" }));
    await user.click(screen.getByRole("button", { name: "Done" }));
    await user.click(screen.getByRole("button", { name: "Save profile" }));

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        siteId: "101",
        siteIds: ["101", "102"],
        siteNamesById: { "101": "Austin, TX", "102": "Denver, CO" },
        siteSelection: "allSites",
        scopeType: "site",
        scopeId: "All sites",
        deviceSetIds: [],
        deviceIdentifiers: [],
      }),
    );
  });

  it("renders edit mode with the full plan visible and locked where fields are not updateable", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      mode: "edit",
      initialValues: {
        ...configuredValues,
        facilityFanDeviceIds: ["31", "32"],
        includeMaintenance: false,
      },
    });

    expect(screen.getByRole("dialog", { name: "Manage curtailment" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Curtailment mode" })).toBeDisabled();
    expect(screen.getByLabelText("Fixed target reduction (kW)")).toBeDisabled();
    expect(screen.getByTestId("curtailment-curtail-batch-size")).toBeDisabled();
    expect(screen.getByTestId("curtailment-curtail-batch-interval")).toBeDisabled();
    expect(screen.getByRole("button", { name: /Miners\s+Select/ })).toBeDisabled();
    expect(screen.getByRole("button", { name: /Infrastructure\s+2 devices/ })).toBeDisabled();
    expect(screen.queryByText("Include miners in maintenance")).not.toBeInTheDocument();

    const saveButton = screen.getByRole("button", { name: "Save" });
    expect(saveButton).toBeDisabled();

    await user.clear(screen.getByTestId("curtailment-restore-batch-interval"));
    await user.type(screen.getByTestId("curtailment-restore-batch-interval"), "180");
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
    expect(screen.getByTestId("curtailment-restore-batch-interval")).toHaveValue("120");
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

    await user.clear(screen.getByTestId("curtailment-restore-batch-interval"));

    expect(screen.getByText("Restore interval cannot be cleared.")).toBeInTheDocument();
    expect(saveButton).toBeEnabled();

    await user.click(saveButton);
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("allows zero restore interval in edit mode", async () => {
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

    await user.clear(screen.getByTestId("curtailment-restore-batch-interval"));
    await user.type(screen.getByTestId("curtailment-restore-batch-interval"), "0");

    expect(screen.queryByText("Enter batch interval greater than 0.")).not.toBeInTheDocument();
    expect(saveButton).toBeEnabled();

    await user.click(saveButton);
    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ restoreIntervalSec: "0" }));
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

    await user.clear(screen.getByTestId("curtailment-restore-batch-interval"));
    await user.type(screen.getByTestId("curtailment-restore-batch-interval"), "180");
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

  it("renders selected infrastructure in the curtailment preview", () => {
    mockUseCurtailmentPlanPreview.mockReturnValue({
      preview: {
        ...preview,
        selectedMinerCount: 2,
        facilityFanDeviceCount: 1,
        scopeLabel: "from selected miners",
      },
      previewError: undefined,
      isPreviewLoading: false,
    });

    renderModal({
      initialValues: {
        ...configuredValues,
        deviceIdentifiers: ["miner-1", "miner-2"],
        facilityFanDeviceIds: ["31"],
      },
    });

    expect(screen.getAllByText("Curtail 2 miners, 1 fan from selected miners immediately")).toHaveLength(2);
  });

  it("renders singular mixed-scope preview copy without double-counting selected miners", () => {
    mockUseCurtailmentPlanPreview.mockReturnValue({
      preview: {
        ...preview,
        selectedMinerCount: 1,
        scopeLabel: "from Calgary and selected miners",
      },
      previewError: undefined,
      isPreviewLoading: false,
    });

    renderModal({ initialValues: configuredValues });

    expect(screen.getAllByText("Curtail 1 miner from Calgary and selected miners immediately")).toHaveLength(2);
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

    // Full shutdown is the default; switch to fixed-kW mode first.
    await user.click(screen.getByRole("button", { name: "Curtailment mode" }));
    await user.click(await screen.findByText("Fixed kW reduction"));

    const targetInput = screen.getByLabelText("Fixed target reduction (kW)");
    const restoreBatchSizeInput = screen.getAllByLabelText("Batch size (miners)")[1];
    const restoreIntervalInput = screen.getAllByLabelText("Batch interval (sec)")[1];

    await user.type(targetInput, "75");
    await user.type(restoreBatchSizeInput, "10");
    await user.type(restoreIntervalInput, "120");
    await user.type(screen.getByLabelText("Reason"), "Grid response");
    await user.click(screen.getByRole("button", { name: "Run curtailment" }));

    expect(onSubmit).not.toHaveBeenCalled();
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
      includeMaintenance: false,
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

    // Full shutdown is the default mode — no mode switch needed, and no
    // fixed-target input is required to start.
    expect(screen.getByRole("button", { name: "Curtailment mode" })).toHaveTextContent("Full shutdown");
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

  it("hides the maintenance option and excludes maintenance miners by default", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({ initialValues: configuredValues });

    expect(screen.queryByText("Include miners in maintenance")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Run curtailment" }));
    expect(screen.queryByText("Force include all paired miners?")).not.toBeInTheDocument();
    expect(screen.getByText("Run curtailment?")).toBeInTheDocument();
    await confirmCurtailment(user);
    // force_include_maintenance is admin-gated server-side; defaulting it off
    // keeps non-admin operators with curtailment:manage able to start.
    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ includeMaintenance: false }));
  });

  it("confirms targeting all paired miners in full-shutdown mode", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      initialValues: { ...configuredValues, curtailmentMode: "fullFleet", targetKw: "" },
    });

    expect(getAllPairedCheckbox()).not.toBeChecked();

    await user.click(screen.getByText("Target all paired miners"));
    expect(screen.getByText("Force include all paired miners?")).toBeInTheDocument();
    expect(
      screen.getByText(
        "This will keep targeting paired miners even when they are offline, sleeping, or waiting for authentication, and includes miners flagged for maintenance.",
      ),
    ).toBeInTheDocument();
    expect(getAllPairedCheckbox()).not.toBeChecked();

    await user.click(screen.getByRole("button", { name: "Force include" }));
    await waitFor(() => expect(screen.queryByText("Force include all paired miners?")).not.toBeInTheDocument());
    expect(getAllPairedCheckbox()).toBeChecked();

    await user.click(screen.getByRole("button", { name: "Run curtailment" }));
    expect(screen.getByText("Run curtailment?")).toBeInTheDocument();
    await confirmCurtailment(user);
    // The maintenance opt-in coupling happens in the request builders, not
    // the form values: submitted values carry the raw checkbox state.
    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        curtailmentMode: "fullFleet",
        forceIncludeAllPairedMiners: true,
        includeMaintenance: false,
      }),
    );
  });

  it("hides the all-paired option for explicit miner selections", () => {
    renderModal({
      initialValues: {
        ...configuredValues,
        curtailmentMode: "fullFleet",
        targetKw: "",
        scopeType: "explicitMiners",
        deviceIdentifiers: ["miner-1"],
      },
    });

    // Explicit miner selections map to an open-loop device-list scope, which
    // the all-paired policy does not support (the server rejects it).
    expect(screen.queryByText("Target all paired miners")).not.toBeInTheDocument();
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

  it("treats all-miner selection as whole fleet without submitting page-loaded miner ids", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      initialValues: { ...configuredValues, includeMaintenance: false },
      siteOptions,
    });

    await user.click(screen.getByRole("button", { name: /Miners\s+Select/ }));
    await user.click(screen.getByRole("button", { name: "Save all miners" }));

    expect(screen.getByRole("button", { name: /Miners\s+All miners/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Sites\s+All sites/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Run curtailment" }));
    expect(
      screen.getByText(
        "This will curtail the whole fleet immediately. Schedules stay suppressed until miners are restored.",
      ),
    ).toBeInTheDocument();
    await confirmCurtailment(user);

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        scopeType: "wholeOrg",
        scopeId: "whole-org",
        siteSelection: "allSites",
        siteId: "",
        deviceSetIds: [],
        deviceIdentifiers: [],
        minerSelectionMode: "all",
      }),
    );
  });

  it("treats filtered all-miner selection as all miners instead of submitting page-loaded ids", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      initialValues: { ...configuredValues, includeMaintenance: false },
      siteOptions,
    });

    await user.click(screen.getByRole("button", { name: /Miners\s+Select/ }));
    await user.click(screen.getByRole("button", { name: "Save filtered all miners" }));

    expect(screen.getByRole("button", { name: /Miners\s+All miners/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Sites\s+All sites/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Run curtailment" }));
    await confirmCurtailment(user);

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        scopeType: "wholeOrg",
        scopeId: "whole-org",
        siteSelection: "allSites",
        siteId: "",
        deviceSetIds: [],
        deviceIdentifiers: [],
        minerSelectionMode: "all",
      }),
    );
  });

  it("preserves all-sites selection when saving a miner subset", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      initialValues: { ...configuredValues, includeMaintenance: false },
      siteOptions,
    });

    await user.click(screen.getByRole("button", { name: /Sites\s+Select/ }));
    await user.click(screen.getByRole("button", { name: "Select all" }));
    await user.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.getByRole("button", { name: /Sites\s+All sites/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /Miners\s+Select/ }));
    await user.click(screen.getByRole("button", { name: "Save miners" }));

    expect(screen.getByRole("button", { name: /Sites\s+All sites/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Miners\s+3 miners/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Run curtailment" }));
    await confirmCurtailment(user);

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        scopeType: "explicitMiners",
        scopeId: "All sites",
        siteSelection: "allSites",
        siteId: "101",
        siteIds: ["101", "102"],
        siteNamesById: { "101": "Austin, TX", "102": "Denver, CO" },
        deviceSetIds: [],
        deviceIdentifiers: ["miner-1", "miner-2", "miner-3"],
        minerSelectionMode: "subset",
      }),
    );
  });

  it("preserves a miner subset when saving all sites", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      initialValues: {
        ...configuredValues,
        includeMaintenance: false,
        scopeType: "explicitMiners",
        deviceIdentifiers: ["miner-1", "miner-2"],
        minerSelectionMode: "subset",
      },
      siteOptions,
    });

    expect(screen.getByRole("button", { name: /Miners\s+2 miners/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /Sites\s+Select/ }));
    await user.click(screen.getByRole("button", { name: "Select all" }));
    await user.click(screen.getByRole("button", { name: "Done" }));

    expect(screen.getByRole("button", { name: /Sites\s+All sites/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Miners\s+2 miners/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Run curtailment" }));
    await confirmCurtailment(user);

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        scopeType: "explicitMiners",
        scopeId: "All sites",
        siteSelection: "allSites",
        siteId: "101",
        siteIds: ["101", "102"],
        siteNamesById: { "101": "Austin, TX", "102": "Denver, CO" },
        deviceSetIds: [],
        deviceIdentifiers: ["miner-1", "miner-2"],
        minerSelectionMode: "subset",
      }),
    );
  });

  it("clears all-miners mode when saving a site subset", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal({
      initialValues: { ...configuredValues, includeMaintenance: false },
      siteOptions,
    });

    await user.click(screen.getByRole("button", { name: /Miners\s+Select/ }));
    await user.click(screen.getByRole("button", { name: "Save all miners" }));
    expect(screen.getByRole("button", { name: /Miners\s+All miners/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Sites\s+All sites/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /Sites\s+All sites/ }));
    await user.click(screen.getByRole("button", { name: "Select none" }));
    await user.click(screen.getByTestId("response-profile-scope-site-101"));
    await user.click(screen.getByRole("button", { name: "Done" }));

    expect(screen.getByRole("button", { name: /Sites\s+Austin, TX/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Miners\s+Select/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Run curtailment" }));
    await confirmCurtailment(user);

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        scopeType: "site",
        scopeId: "Austin, TX",
        siteSelection: "site",
        siteId: "101",
        siteIds: ["101"],
        deviceSetIds: [],
        deviceIdentifiers: [],
        minerSelectionMode: "subset",
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
    expect(saveButton).toBeEnabled();

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
        initialValues={{ curtailmentMode: "fixedKwReduction", targetKw: "10", reason: "Initial reason" }}
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
        initialValues={{ curtailmentMode: "fixedKwReduction", targetKw: "10", reason: "Initial reason" }}
      />,
    );
    rerender(
      <CurtailmentStartModal
        open
        onDismiss={onDismiss}
        onSubmit={onSubmit}
        initialValues={{ curtailmentMode: "fixedKwReduction", targetKw: "25", reason: "Updated reason" }}
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

  it("shows required start-field validation when the CTA is clicked or fields are edited", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderModal();

    // Full shutdown is the default; switch to fixed-kW mode so the
    // target-reduction requirement applies.
    await user.click(screen.getByRole("button", { name: "Curtailment mode" }));
    await user.click(await screen.findByText("Fixed kW reduction"));

    const startButton = screen.getByRole("button", { name: "Run curtailment" });
    const targetInput = screen.getByLabelText("Fixed target reduction (kW)");
    const reasonInput = screen.getByLabelText("Reason");

    expect(screen.queryByText("Enter a target reduction.")).not.toBeInTheDocument();
    expect(screen.queryByText("Enter a reason.")).not.toBeInTheDocument();
    expect(startButton).toBeEnabled();

    await user.click(startButton);

    expect(onSubmit).not.toHaveBeenCalled();
    expect(reasonInput).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByText("Enter a reason.")).toBeInTheDocument();
    expect(targetInput).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByText("Enter a target reduction.")).toBeInTheDocument();

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
