import { MemoryRouter } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import type { SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { useSites } from "@/protoFleet/api/sites";
import { useCurtailmentApi } from "@/protoFleet/api/useCurtailmentApi";
import useCurtailmentAutomationRules from "@/protoFleet/api/useCurtailmentAutomationRules";
import useCurtailmentResponseProfiles from "@/protoFleet/api/useCurtailmentResponseProfiles";
import useInfrastructureDevices from "@/protoFleet/api/useInfrastructureDevices";
import useMqttCurtailmentSources from "@/protoFleet/api/useMqttCurtailmentSources";
import type { CurtailmentFormValues, CurtailmentPlanPreview } from "@/protoFleet/features/energy/CurtailmentStartModal";
import CurtailmentSettingsPage, {
  CurtailmentSettingsContent,
} from "@/protoFleet/features/settings/components/Curtailment";
import type {
  AutomationRule,
  CurtailmentSource,
  CurtailmentSourceFormValues,
  ResponseProfile,
} from "@/protoFleet/features/settings/components/Curtailment/types";
import { useHasPermission } from "@/protoFleet/store";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";
import { pushToast } from "@/shared/features/toaster";

const { activeSiteMock, mockNavigate, mockUseCurtailmentPlanPreview } = vi.hoisted(() => ({
  activeSiteMock: { current: { kind: "all" } as { kind: string; id?: string; slug?: string } },
  mockNavigate: vi.fn(),
  mockUseCurtailmentPlanPreview: vi.fn(),
}));

vi.mock("react-router-dom", async () => {
  const actual = await vi.importActual<typeof import("react-router-dom")>("react-router-dom");

  return {
    ...actual,
    useNavigate: () => mockNavigate,
  };
});

vi.mock("@/protoFleet/store", () => ({
  useHasPermission: vi.fn(),
}));

vi.mock("@/protoFleet/api/sites", () => ({
  useSites: vi.fn(),
}));

vi.mock("@/protoFleet/components/PageHeader/SitePicker", () => ({
  useActiveSite: () => ({ activeSite: activeSiteMock.current, setActiveSite: vi.fn() }),
}));

vi.mock("@/protoFleet/api/useMqttCurtailmentSources", () => ({
  default: vi.fn(),
}));

vi.mock("@/protoFleet/api/useCurtailmentResponseProfiles", () => ({
  default: vi.fn(),
  getResponseProfileScopeLabelForActionType: () => "Whole fleet",
}));

vi.mock("@/protoFleet/api/useInfrastructureDevices", () => ({
  default: vi.fn(),
}));

vi.mock("@/protoFleet/api/useCurtailmentAutomationRules", () => ({
  default: vi.fn(),
}));

vi.mock("@/protoFleet/api/useCurtailmentApi", () => ({
  useCurtailmentApi: vi.fn(),
}));

vi.mock("@/protoFleet/features/energy/useCurtailmentPlanPreview", () => ({
  createCurtailmentPlanPreview: (
    values: CurtailmentFormValues,
    source: { selectedMinerCount: number; targetKw?: number; estimatedReductionKw: number },
  ): CurtailmentPlanPreview => ({
    selectedMinerCount: source.selectedMinerCount,
    targetKw: source.targetKw ?? Number(values.targetKw || "0"),
    estimatedReductionKw: source.estimatedReductionKw,
    curtailEstimate: "~1 minute",
    restoreEstimate: "~2 minutes",
    scopeLabel: values.scopeId ?? "across the fleet",
  }),
  getUnsupportedDeviceSetPreviewError: () => undefined,
  useCurtailmentPlanPreview: mockUseCurtailmentPlanPreview,
}));

vi.mock("@/protoFleet/features/settings/components/Schedules/MinerSelectionModal", () => ({
  default: ({ open, onSave }: { open: boolean; onSave: (minerIds: string[]) => void }) =>
    open ? (
      <div role="dialog" aria-label="Select miners">
        <button type="button" onClick={() => onSave(["miner-1", "miner-2"])}>
          Save miners
        </button>
      </div>
    ) : null,
}));

vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(),
  STATUSES: {
    error: "error",
    success: "success",
  },
}));

const testSources: CurtailmentSource[] = [
  {
    id: "site-alpha-mqtt",
    name: "Site Alpha MQTT",
    triggerType: "MQTT",
    brokerHosts: ["site-alpha-primary.broker.test", "site-alpha-secondary.broker.test"],
    port: 11883,
    topic: "curtailment/site-alpha/target",
    protocol: "MQTT 3.1.1",
    qos: 1,
    username: "curtailment-alpha",
    lastTarget: "0",
    lastSeen: "38 seconds ago",
    health: "connected",
    enabled: true,
    stalenessThresholdSec: 240,
  },
  {
    id: "site-beta-mqtt",
    name: "Site Beta MQTT",
    triggerType: "MQTT",
    brokerHosts: ["site-beta-primary.broker.test", "site-beta-secondary.broker.test"],
    port: 11884,
    topic: "curtailment/site-beta/target",
    protocol: "MQTT 3.1.1",
    qos: 1,
    username: "curtailment-beta",
    lastTarget: "100",
    lastSeen: "24 seconds ago",
    health: "connected",
    enabled: true,
    stalenessThresholdSec: 240,
  },
  {
    id: "site-gamma-mqtt",
    name: "Site Gamma MQTT",
    triggerType: "MQTT",
    brokerHosts: ["site-gamma-primary.broker.test", "site-gamma-secondary.broker.test"],
    port: 11885,
    topic: "curtailment/site-gamma/target",
    protocol: "MQTT 3.1.1",
    qos: 1,
    username: "curtailment-gamma",
    lastTarget: "-",
    lastSeen: "-",
    health: "waitingForSignal",
    enabled: true,
    stalenessThresholdSec: 240,
  },
  {
    id: "site-delta-mqtt",
    name: "Site Delta MQTT",
    triggerType: "MQTT",
    brokerHosts: ["site-delta-primary.broker.test", "site-delta-secondary.broker.test"],
    port: 11886,
    topic: "curtailment/site-delta/target",
    protocol: "MQTT 3.1.1",
    qos: 1,
    username: "curtailment-delta",
    lastTarget: "-",
    lastSeen: "12 minutes ago",
    health: "noSignal",
    enabled: true,
    stalenessThresholdSec: 240,
  },
];

const apiSources: CurtailmentSource[] = [
  {
    ...testSources[0],
    id: "11",
    hasPassword: true,
  },
];

const testResponseProfiles: ResponseProfile[] = [
  {
    id: "emergency-full-shed",
    name: "Emergency full shed",
    targetSummary: "100% reduction",
    scope: "Whole fleet",
    selectionStrategy: "Least efficient first",
    restoreBehavior: "Restore in batches",
    deadlineSummary: "Within 5 min",
    formValues: {
      name: "Emergency full shed",
      actionType: "fullFleet",
      targetKw: "",
      deviceIdentifiers: [],
      siteId: "",
      siteName: "",
      selectionStrategy: "leastEfficientFirst",
      restoreBehavior: "automaticBatchRestore",
      minDurationSec: "",
      maxDurationSec: "300",
      curtailBatchSize: "",
      curtailBatchIntervalSec: "",
      restoreBatchSize: "",
      restoreIntervalSec: "",
      responseDeadlineMinutes: "5",
      includeMaintenance: false,
    },
  },
  {
    id: "site-alpha-500-kw",
    name: "Site Alpha 500 kW",
    targetSummary: "500 kW target",
    scope: "Whole fleet",
    selectionStrategy: "Least efficient first",
    restoreBehavior: "Restore immediately",
    deadlineSummary: "Within 15 min",
    formValues: {
      name: "Site Alpha 500 kW",
      actionType: "fixedKwReduction",
      targetKw: "500",
      deviceIdentifiers: [],
      siteId: "",
      siteName: "",
      selectionStrategy: "leastEfficientFirst",
      restoreBehavior: "automaticImmediateRestore",
      minDurationSec: "",
      maxDurationSec: "900",
      curtailBatchSize: "50",
      curtailBatchIntervalSec: "30",
      restoreBatchSize: "0",
      restoreIntervalSec: "0",
      responseDeadlineMinutes: "15",
      includeMaintenance: false,
    },
  },
];

const targetedMinersResponseProfile: ResponseProfile = {
  id: "targeted-miners",
  name: "Targeted miners",
  targetSummary: "650 kW target",
  scope: "Whole fleet",
  selectionStrategy: "Least efficient first",
  restoreBehavior: "Restore in batches",
  deadlineSummary: "Within 15 min",
  formValues: {
    name: "Targeted miners",
    actionType: "fixedKwReduction",
    targetKw: "650",
    deviceIdentifiers: ["miner-1", "miner-2", "miner-3"],
    siteId: "",
    siteName: "",
    selectionStrategy: "leastEfficientFirst",
    restoreBehavior: "automaticBatchRestore",
    minDurationSec: "",
    maxDurationSec: "900",
    curtailBatchSize: "10",
    curtailBatchIntervalSec: "60",
    restoreBatchSize: "10",
    restoreIntervalSec: "120",
    responseDeadlineMinutes: "15",
    includeMaintenance: false,
  },
};

const siteScopedResponseProfile: ResponseProfile = {
  id: "site-scoped-profile",
  name: "Site scoped profile",
  targetSummary: "400 kW target",
  scope: "Site 101",
  selectionStrategy: "Least efficient first",
  restoreBehavior: "Restore immediately",
  deadlineSummary: "Within 15 min",
  formValues: {
    name: "Site scoped profile",
    actionType: "fixedKwReduction",
    targetKw: "400",
    deviceIdentifiers: [],
    siteId: "101",
    siteName: "Site 101",
    selectionStrategy: "leastEfficientFirst",
    restoreBehavior: "automaticImmediateRestore",
    minDurationSec: "",
    maxDurationSec: "900",
    curtailBatchSize: "40",
    curtailBatchIntervalSec: "30",
    restoreBatchSize: "0",
    restoreIntervalSec: "0",
    responseDeadlineMinutes: "15",
    includeMaintenance: false,
  },
};

const testAutomationRules: AutomationRule[] = [
  {
    id: "site-alpha-automation",
    priority: 1,
    name: "Site Alpha automation",
    conditionType: "mqttTriggerTargetOff",
    conditionSummary: "Site Alpha MQTT grid signal changes to 0",
    sourceId: "site-alpha-mqtt",
    responseProfileId: "emergency-full-shed",
    enabled: true,
  },
  {
    id: "site-beta-automation",
    priority: 2,
    name: "Site Beta automation",
    conditionType: "mqttTriggerTargetOff",
    conditionSummary: "Site Beta MQTT grid signal changes to 0",
    sourceId: "site-beta-mqtt",
    responseProfileId: "site-alpha-500-kw",
    enabled: false,
  },
];

const apiResponseProfiles: ResponseProfile[] = [
  {
    ...testResponseProfiles[0],
    id: "21",
  },
];

const apiAutomationRules: AutomationRule[] = [
  {
    ...testAutomationRules[0],
    id: "7",
    conditionSummary: "Site Alpha MQTT grid signal changes to 0",
    sourceId: "11",
    responseProfileId: "21",
    responseProfileName: "Emergency full shed",
  },
];

const testSourceFormValues: CurtailmentSourceFormValues = {
  name: "Site Alpha MQTT",
  brokerPrimaryHost: "site-alpha-primary.broker.test",
  brokerSecondaryHost: "site-alpha-secondary.broker.test",
  brokerPort: "11883",
  topic: "curtailment/site-alpha/target",
  username: "curtailment-alpha",
  password: "secret",
  stalenessThresholdSec: "240",
};

const createSourceMock = vi.fn();
const updateSourceMock = vi.fn();
const testConnectionMock = vi.fn();
const setSourceEnabledMock = vi.fn();
const deleteSourceMock = vi.fn();
const createResponseProfileMock = vi.fn();
const updateResponseProfileMock = vi.fn();
const deleteResponseProfileMock = vi.fn();
const createAutomationRuleMock = vi.fn();
const updateAutomationRuleMock = vi.fn();
const setAutomationRuleEnabledMock = vi.fn();
const deleteAutomationRuleMock = vi.fn();
const startCurtailmentMock = vi.fn();
const listSitesMock = vi.fn();
const listInfrastructureDevicesMock = vi.fn();

const mockResponseProfilesApi = (overrides: Partial<ReturnType<typeof useCurtailmentResponseProfiles>> = {}) => {
  vi.mocked(useCurtailmentResponseProfiles).mockReturnValue({
    responseProfiles: [],
    isLoading: false,
    isCreating: false,
    updatingProfileIds: new Set<string>(),
    loadError: null,
    createError: null,
    listResponseProfiles: vi.fn(),
    createResponseProfile: createResponseProfileMock,
    updateResponseProfile: updateResponseProfileMock,
    deleteResponseProfile: deleteResponseProfileMock,
    ...overrides,
  });
};

const mockSourcesApi = (overrides: Partial<ReturnType<typeof useMqttCurtailmentSources>> = {}) => {
  vi.mocked(useMqttCurtailmentSources).mockReturnValue({
    sources: [],
    isLoading: false,
    isCreating: false,
    updatingSourceIds: new Set<string>(),
    loadError: null,
    createError: null,
    listSources: vi.fn(),
    createSource: createSourceMock,
    updateSource: updateSourceMock,
    testConnection: testConnectionMock,
    isTestingConnection: false,
    setSourceEnabled: setSourceEnabledMock,
    deleteSource: deleteSourceMock,
    ...overrides,
  });
};

const mockAutomationRulesApi = (overrides: Partial<ReturnType<typeof useCurtailmentAutomationRules>> = {}) => {
  vi.mocked(useCurtailmentAutomationRules).mockReturnValue({
    automationRules: [],
    isLoading: false,
    isCreating: false,
    updatingRuleIds: new Set<string>(),
    loadError: null,
    createError: null,
    listAutomationRules: vi.fn(),
    createAutomationRule: createAutomationRuleMock,
    updateAutomationRule: updateAutomationRuleMock,
    setAutomationRuleEnabled: setAutomationRuleEnabledMock,
    deleteAutomationRule: deleteAutomationRuleMock,
    ...overrides,
  });
};

function fillSourceForm(values: CurtailmentSourceFormValues = testSourceFormValues): void {
  fireEvent.change(screen.getByLabelText("Configuration name"), { target: { value: values.name } });
  fireEvent.change(screen.getByLabelText("Broker host 1"), { target: { value: values.brokerPrimaryHost } });
  fireEvent.change(screen.getByLabelText("Broker host 2"), { target: { value: values.brokerSecondaryHost } });
  fireEvent.change(screen.getByLabelText("Port"), { target: { value: values.brokerPort } });
  fireEvent.change(screen.getByLabelText("Topic"), { target: { value: values.topic } });
  fireEvent.change(screen.getByLabelText("Username"), { target: { value: values.username } });
  fireEvent.change(screen.getByLabelText("Password"), { target: { value: values.password } });
  fireEvent.change(screen.getByLabelText("No signal timeout"), { target: { value: values.stalenessThresholdSec } });
}

function getSourceRow(sourceName: string): HTMLTableRowElement {
  const row = screen.getByText(sourceName).closest("tr");
  expect(row).not.toBeNull();
  return row as HTMLTableRowElement;
}

function getResponseProfileCard(profileName: string): HTMLElement {
  const card = screen.getByText(profileName).closest(".rounded-xl");
  expect(card).not.toBeNull();
  return card as HTMLElement;
}

function getEnabledButton(name: string): HTMLButtonElement {
  const button = screen
    .getAllByRole("button", { name })
    .find((element): element is HTMLButtonElement => element instanceof HTMLButtonElement && !element.disabled);

  if (!button) {
    throw new Error(`No enabled ${name} button found`);
  }

  return button;
}

function confirmCurtailmentAction(name = "Run curtailment"): void {
  fireEvent.click(within(screen.getByTestId("curtailment-run-confirmation")).getByRole("button", { name }));
}

function makeSiteWithCounts(id: bigint, name: string): SiteWithCounts {
  return { site: { id, name } } as SiteWithCounts;
}

function mockSitesApi() {
  vi.mocked(useSites).mockReturnValue({
    listSites: listSitesMock,
  } as Partial<ReturnType<typeof useSites>> as ReturnType<typeof useSites>);
  listSitesMock.mockImplementation(
    ({ onSuccess, onFinally }: { onSuccess?: (sites: SiteWithCounts[]) => void; onFinally?: () => void } = {}) => {
      onSuccess?.([makeSiteWithCounts(101n, "Austin, TX"), makeSiteWithCounts(102n, "Denver, CO")]);
      onFinally?.();
    },
  );
}

describe("CurtailmentSettingsPage", () => {
  beforeEach(() => {
    activeSiteMock.current = { kind: "all" };
    useFleetStore.getState().ui.setActiveSite({ kind: "all" });
    vi.mocked(useHasPermission).mockReset();
    vi.mocked(useMqttCurtailmentSources).mockReset();
    vi.mocked(useSites).mockReset();
    vi.mocked(useCurtailmentResponseProfiles).mockReset();
    vi.mocked(useInfrastructureDevices).mockReset();
    vi.mocked(useCurtailmentAutomationRules).mockReset();
    vi.mocked(useCurtailmentApi).mockReset();
    vi.mocked(pushToast).mockReset();
    mockNavigate.mockReset();
    createSourceMock.mockReset();
    updateSourceMock.mockReset();
    testConnectionMock.mockReset();
    setSourceEnabledMock.mockReset();
    deleteSourceMock.mockReset();
    mockUseCurtailmentPlanPreview.mockReset();
    mockUseCurtailmentPlanPreview.mockReturnValue({
      preview: undefined,
      previewError: undefined,
      isPreviewLoading: false,
    });
    createResponseProfileMock.mockReset();
    updateResponseProfileMock.mockReset();
    deleteResponseProfileMock.mockReset();
    createAutomationRuleMock.mockReset();
    updateAutomationRuleMock.mockReset();
    setAutomationRuleEnabledMock.mockReset();
    deleteAutomationRuleMock.mockReset();
    startCurtailmentMock.mockReset();
    listSitesMock.mockReset();
    listInfrastructureDevicesMock.mockReset();
    startCurtailmentMock.mockResolvedValue({});
    vi.mocked(useCurtailmentApi).mockReturnValue({
      startCurtailment: startCurtailmentMock,
    } as Partial<ReturnType<typeof useCurtailmentApi>> as ReturnType<typeof useCurtailmentApi>);
    mockResponseProfilesApi();
    mockSourcesApi();
    mockSitesApi();
    mockAutomationRulesApi();
    vi.mocked(useInfrastructureDevices).mockReturnValue({
      devices: [],
      isLoading: false,
      loadError: null,
      updatingDeviceIds: new Set<string>(),
      listDevices: listInfrastructureDevicesMock,
      createDevice: vi.fn(),
      updateDevice: vi.fn(),
      setDeviceEnabled: vi.fn(),
      deleteDevice: vi.fn(),
    });
  });

  it("renders the curtailment header and section null states", () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage");

    render(
      <MemoryRouter>
        <CurtailmentSettingsPage />
      </MemoryRouter>,
    );

    expect(useHasPermission).toHaveBeenCalledWith("curtailment:manage");
    expect(useCurtailmentResponseProfiles).toHaveBeenCalledWith(true, { siteNameById: undefined });
    expect(useMqttCurtailmentSources).toHaveBeenCalledWith(true);
    expect(useCurtailmentAutomationRules).toHaveBeenCalledWith(true);
    expect(screen.getByTestId("settings-curtailment-page")).toBeVisible();
    expect(screen.getByText("Curtailment")).toBeVisible();
    expect(
      screen.getByText(
        "Configure response profiles, manage external signal sources, and define automations that trigger curtailment.",
      ),
    ).toBeVisible();
    expect(screen.getByText("Response profiles")).toBeVisible();
    expect(screen.getByRole("button", { name: "About response profiles" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Create profile" })).toBeEnabled();
    expect(screen.getByText("Sources")).toBeVisible();
    expect(screen.getByRole("button", { name: "About sources" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Add source" })).toBeEnabled();
    expect(screen.getByText("Automations")).toBeVisible();
    expect(screen.getByRole("button", { name: "About automations" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Create automation" })).toBeEnabled();
    expect(screen.queryByRole("button", { name: "Save settings" })).not.toBeInTheDocument();
    expect(document.querySelector(".curtailment-section-header__icon")).not.toBeInTheDocument();
    expect(screen.getByText("No sources configured")).toBeVisible();
    expect(screen.getByText("No automations configured")).toBeVisible();
    expect(screen.queryByRole("columnheader", { name: "Name" })).not.toBeInTheDocument();
    expect(screen.queryByRole("columnheader", { name: "Condition" })).not.toBeInTheDocument();
    expect(screen.queryByRole("columnheader", { name: "Enabled" })).not.toBeInTheDocument();
    for (const profileColumnName of ["Target", "Scope", "Selection", "Restore", "Deadline"]) {
      expect(screen.queryByRole("columnheader", { name: profileColumnName })).not.toBeInTheDocument();
    }
    expect(screen.queryByRole("columnheader", { name: "Last target" })).not.toBeInTheDocument();
    expect(screen.queryByRole("columnheader", { name: "Type" })).not.toBeInTheDocument();
    expect(screen.queryByRole("columnheader", { name: "Broker hosts" })).not.toBeInTheDocument();
    expect(screen.queryByText("Site Alpha MQTT")).not.toBeInTheDocument();
    expect(screen.queryByText("Site Beta MQTT")).not.toBeInTheDocument();
    expect(screen.queryByTestId("list-empty-row")).not.toBeInTheDocument();
    expect(screen.getByText("No response profiles configured")).toBeVisible();
    expect(screen.getByText("Add a profile to reuse curtailment actions across automation rules.")).toBeVisible();
    expect(screen.getByText("No sources configured")).toBeVisible();
    expect(screen.getByText("Add a MaestroOS MQTT source to receive curtailment signals.")).toBeVisible();
    expect(screen.getByText("No automations configured")).toBeVisible();
    expect(screen.getByText("Add an automation to trigger a response profile.")).toBeVisible();
  });

  it("renders sources returned by the API hook", () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage");
    mockSourcesApi({ sources: apiSources });

    render(
      <MemoryRouter>
        <CurtailmentSettingsPage />
      </MemoryRouter>,
    );

    expect(screen.getByText("Site Alpha MQTT")).toBeVisible();
    expect(screen.getByText("38 seconds ago")).toBeVisible();
  });

  it("renders response profiles returned by the API hook", () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage");
    mockResponseProfilesApi({ responseProfiles: testResponseProfiles });

    render(
      <MemoryRouter>
        <CurtailmentSettingsPage />
      </MemoryRouter>,
    );

    expect(screen.getByText("Emergency full shed")).toBeVisible();
    expect(screen.getByText("100% reduction")).toBeVisible();
    expect(within(getResponseProfileCard("Emergency full shed")).getByText("Whole fleet")).toBeVisible();
  });

  it("populates the response profile fan selector with infrastructure devices from the shared hook", async () => {
    const user = userEvent.setup();
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage" || key === "site:read");
    vi.mocked(useInfrastructureDevices).mockReturnValue({
      devices: [
        {
          id: "31",
          siteId: "101",
          siteName: "Austin, TX",
          buildingName: "Building 1",
          name: "Fan Unit 1",
          deviceKind: "single_fan",
          fanCount: 1,
          enabled: true,
          driverType: "modbus",
          driverConfig: "",
        },
      ],
      isLoading: false,
      loadError: null,
      updatingDeviceIds: new Set<string>(),
      listDevices: listInfrastructureDevicesMock,
      createDevice: vi.fn(),
      updateDevice: vi.fn(),
      setDeviceEnabled: vi.fn(),
      deleteDevice: vi.fn(),
    });

    render(
      <MemoryRouter>
        <CurtailmentSettingsPage />
      </MemoryRouter>,
    );

    expect(useInfrastructureDevices).toHaveBeenCalledWith(true, undefined, true);
    await user.click(screen.getByRole("button", { name: "Create profile" }));
    await user.click(screen.getByRole("button", { name: /Infrastructure\s+Select/ }));

    expect(screen.getByRole("checkbox", { name: /Fan Unit 1/ })).toBeVisible();
  });

  it("loads site names for site-scoped response profile cards after reload", async () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage" || key === "site:read");
    vi.mocked(useCurtailmentResponseProfiles).mockImplementation((_enabled, options) => {
      const siteName = options?.siteNameById?.get("101") ?? "Site 101";

      return {
        responseProfiles: [
          {
            ...siteScopedResponseProfile,
            scope: siteName,
            formValues: {
              ...siteScopedResponseProfile.formValues!,
              siteName,
            },
          },
        ],
        isLoading: false,
        isCreating: false,
        updatingProfileIds: new Set<string>(),
        loadError: null,
        createError: null,
        listResponseProfiles: vi.fn(),
        createResponseProfile: createResponseProfileMock,
        updateResponseProfile: updateResponseProfileMock,
        deleteResponseProfile: deleteResponseProfileMock,
      };
    });

    render(
      <MemoryRouter>
        <CurtailmentSettingsPage />
      </MemoryRouter>,
    );

    await waitFor(() => expect(listSitesMock).toHaveBeenCalled());
    await waitFor(() =>
      expect(within(getResponseProfileCard("Site scoped profile")).getByText("Austin, TX")).toBeVisible(),
    );
  });

  it("does not auto-retry site name loading after ListSites fails", async () => {
    vi.useFakeTimers();
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage" || key === "site:read");
    vi.mocked(useCurtailmentResponseProfiles).mockImplementation(() => ({
      responseProfiles: [siteScopedResponseProfile],
      isLoading: false,
      isCreating: false,
      updatingProfileIds: new Set<string>(),
      loadError: null,
      createError: null,
      listResponseProfiles: vi.fn(),
      createResponseProfile: createResponseProfileMock,
      updateResponseProfile: updateResponseProfileMock,
      deleteResponseProfile: deleteResponseProfileMock,
    }));
    listSitesMock.mockImplementation(
      ({ onError, onFinally }: { onError?: (message: string) => void; onFinally?: () => void } = {}) => {
        onError?.("network down");
        onFinally?.();
      },
    );

    try {
      render(
        <MemoryRouter>
          <CurtailmentSettingsPage />
        </MemoryRouter>,
      );

      await act(async () => {
        await vi.runOnlyPendingTimersAsync();
      });
      expect(listSitesMock).toHaveBeenCalledTimes(1);

      await act(async () => {
        await vi.runOnlyPendingTimersAsync();
      });
      expect(listSitesMock).toHaveBeenCalledTimes(1);
    } finally {
      vi.useRealTimers();
    }
  });

  it("does not auto-retry site name loading after ListSites succeeds with no sites", async () => {
    vi.useFakeTimers();
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage" || key === "site:read");
    vi.mocked(useCurtailmentResponseProfiles).mockImplementation(() => ({
      responseProfiles: [siteScopedResponseProfile],
      isLoading: false,
      isCreating: false,
      updatingProfileIds: new Set<string>(),
      loadError: null,
      createError: null,
      listResponseProfiles: vi.fn(),
      createResponseProfile: createResponseProfileMock,
      updateResponseProfile: updateResponseProfileMock,
      deleteResponseProfile: deleteResponseProfileMock,
    }));
    listSitesMock.mockImplementation(
      ({ onSuccess, onFinally }: { onSuccess?: (sites: SiteWithCounts[]) => void; onFinally?: () => void } = {}) => {
        onSuccess?.([]);
        onFinally?.();
      },
    );

    try {
      render(
        <MemoryRouter>
          <CurtailmentSettingsPage />
        </MemoryRouter>,
      );

      await act(async () => {
        await vi.runOnlyPendingTimersAsync();
      });
      expect(listSitesMock).toHaveBeenCalledTimes(1);

      await act(async () => {
        await vi.runOnlyPendingTimersAsync();
      });
      expect(listSitesMock).toHaveBeenCalledTimes(1);
    } finally {
      vi.useRealTimers();
    }
  });

  it("creates a response profile through the API hook", async () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage");
    createResponseProfileMock.mockResolvedValue(testResponseProfiles[0]);

    render(
      <MemoryRouter>
        <CurtailmentSettingsPage />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Create profile" }));
    expect(screen.getByText("Create response profile")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Profile name"), { target: { value: "Emergency full shed" } });
    expect(screen.queryByRole("checkbox", { name: "Include miners in maintenance" })).not.toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "Target all paired miners" })).not.toBeChecked();
    expect(screen.getByRole("button", { name: /Miners\s+Select/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Sites\s+Select/ })).toBeInTheDocument();
    expect(screen.queryByLabelText("Min duration (sec)")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Max duration (sec)")).not.toBeInTheDocument();
    expect(screen.getAllByLabelText("Batch size (miners)")).toHaveLength(2);
    expect(screen.getAllByLabelText("Batch interval (sec)")).toHaveLength(2);
    fireEvent.change(screen.getByTestId("response-profile-curtail-batch-size"), { target: { value: "25" } });
    fireEvent.change(screen.getByTestId("response-profile-curtail-batch-interval"), { target: { value: "60" } });
    fireEvent.change(screen.getByTestId("response-profile-restore-batch-size"), { target: { value: "10" } });
    fireEvent.change(screen.getByTestId("response-profile-restore-batch-interval"), { target: { value: "120" } });
    fireEvent.click(getEnabledButton("Save profile"));

    await waitFor(() => expect(screen.queryByTestId("full-screen-two-pane-modal")).not.toBeInTheDocument());
    expect(createResponseProfileMock).toHaveBeenCalledWith(
      expect.objectContaining({
        name: "Emergency full shed",
        actionType: "fullFleet",
        siteId: "",
        siteName: "",
        maxDurationSec: "900",
        curtailBatchSize: "25",
        curtailBatchIntervalSec: "60",
        restoreBatchSize: "10",
        restoreIntervalSec: "120",
        responseDeadlineMinutes: "15",
        includeMaintenance: false,
      }),
    );
    expect(pushToast).toHaveBeenCalledWith({
      message: "Response profile added",
      status: "success",
    });
  });

  it("prefills new response profiles with the globally selected site", async () => {
    activeSiteMock.current = { kind: "site", id: "101", slug: "austin" };
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage" || key === "site:read");
    createResponseProfileMock.mockResolvedValue({
      ...testResponseProfiles[0],
      scope: "Austin, TX",
      formValues: {
        ...testResponseProfiles[0].formValues!,
        siteSelection: "site",
        siteId: "101",
        siteName: "Austin, TX",
        siteIds: ["101"],
        siteNamesById: { "101": "Austin, TX" },
      },
    });

    render(
      <MemoryRouter>
        <CurtailmentSettingsPage />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Create profile" }));
    await waitFor(() => expect(screen.getByRole("button", { name: /Sites\s+Austin, TX/ })).toBeInTheDocument());
    fireEvent.change(screen.getByLabelText("Profile name"), { target: { value: "Austin emergency shed" } });
    fireEvent.change(screen.getByTestId("response-profile-curtail-batch-size"), { target: { value: "25" } });
    fireEvent.change(screen.getByTestId("response-profile-curtail-batch-interval"), { target: { value: "60" } });
    fireEvent.change(screen.getByTestId("response-profile-restore-batch-size"), { target: { value: "10" } });
    fireEvent.change(screen.getByTestId("response-profile-restore-batch-interval"), { target: { value: "120" } });
    fireEvent.click(getEnabledButton("Save profile"));

    await waitFor(() =>
      expect(createResponseProfileMock).toHaveBeenCalledWith(
        expect.objectContaining({
          name: "Austin emergency shed",
          siteSelection: "site",
          siteId: "101",
          siteName: "Austin, TX",
          siteIds: ["101"],
          siteNamesById: { "101": "Austin, TX" },
        }),
      ),
    );
  });

  it("creates a site-scoped response profile through the API hook", async () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage" || key === "site:read");
    createResponseProfileMock.mockResolvedValue({
      ...testResponseProfiles[0],
      scope: "Austin, TX",
      formValues: {
        ...testResponseProfiles[0].formValues!,
        siteId: "101",
        siteName: "Austin, TX",
      },
    });

    render(
      <MemoryRouter>
        <CurtailmentSettingsPage />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Create profile" }));
    await waitFor(() => expect(screen.getByRole("button", { name: /Sites\s+Select/ })).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: /Sites\s+Select/ }));
    fireEvent.click(screen.getByTestId("response-profile-scope-site-101"));
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    fireEvent.change(screen.getByLabelText("Profile name"), { target: { value: "Austin emergency shed" } });
    fireEvent.change(screen.getByTestId("response-profile-curtail-batch-size"), { target: { value: "25" } });
    fireEvent.change(screen.getByTestId("response-profile-curtail-batch-interval"), { target: { value: "60" } });
    fireEvent.change(screen.getByTestId("response-profile-restore-batch-size"), { target: { value: "10" } });
    fireEvent.change(screen.getByTestId("response-profile-restore-batch-interval"), { target: { value: "120" } });
    fireEvent.click(getEnabledButton("Save profile"));

    await waitFor(() =>
      expect(createResponseProfileMock).toHaveBeenCalledWith(
        expect.objectContaining({
          name: "Austin emergency shed",
          siteId: "101",
          siteName: "Austin, TX",
          siteIds: ["101"],
          siteNamesById: { "101": "Austin, TX" },
        }),
      ),
    );
  });

  it("keeps the response profile modal open and shows create failures", async () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage");
    createResponseProfileMock.mockRejectedValue(new Error("Profile name already exists"));

    render(
      <MemoryRouter>
        <CurtailmentSettingsPage />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Create profile" }));
    fireEvent.change(screen.getByLabelText("Profile name"), { target: { value: "Emergency full shed" } });
    fireEvent.click(getEnabledButton("Save profile"));

    await waitFor(() =>
      expect(screen.getByTestId("curtailment-action-error")).toHaveTextContent("Profile name already exists"),
    );
    expect(screen.getByText("Create response profile")).toBeInTheDocument();
    expect(screen.getByTestId("full-screen-two-pane-modal")).toBeInTheDocument();
    expect(pushToast).not.toHaveBeenCalledWith({
      message: "Response profile added",
      status: "success",
    });
  });

  it("saves a response profile, runs a curtailment, and redirects to energy", async () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage");
    createResponseProfileMock.mockResolvedValue(testResponseProfiles[0]);

    render(
      <MemoryRouter>
        <CurtailmentSettingsPage />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Create profile" }));
    fireEvent.change(screen.getByLabelText("Profile name"), { target: { value: "Emergency full shed" } });
    fireEvent.change(screen.getByTestId("response-profile-curtail-batch-size"), { target: { value: "25" } });
    fireEvent.change(screen.getByTestId("response-profile-curtail-batch-interval"), { target: { value: "60" } });
    fireEvent.change(screen.getByTestId("response-profile-restore-batch-size"), { target: { value: "10" } });
    fireEvent.change(screen.getByTestId("response-profile-restore-batch-interval"), { target: { value: "120" } });
    fireEvent.click(getEnabledButton("Run curtailment"));
    expect(screen.getByText("Run curtailment?")).toBeInTheDocument();
    expect(
      screen.getByText(
        "This will save the profile, then trigger curtailment for the whole fleet. Schedules stay suppressed until miners are restored.",
      ),
    ).toBeInTheDocument();
    confirmCurtailmentAction();

    await waitFor(() =>
      expect(createResponseProfileMock).toHaveBeenCalledWith(
        expect.objectContaining({
          name: "Emergency full shed",
          actionType: "fullFleet",
          curtailBatchSize: "25",
          curtailBatchIntervalSec: "60",
          restoreBatchSize: "10",
          restoreIntervalSec: "120",
        }),
      ),
    );
    await waitFor(() =>
      expect(startCurtailmentMock).toHaveBeenCalledWith(
        expect.objectContaining({
          reason: "Emergency full shed",
          curtailmentMode: "fullFleet",
          scopeType: "wholeOrg",
          siteId: "",
          curtailBatchSize: "25",
          curtailBatchIntervalSec: "60",
          restoreBatchSize: "10",
          restoreIntervalSec: "120",
          includeMaintenance: false,
        }),
      ),
    );
    expect(mockNavigate).toHaveBeenCalledWith("/energy");
    await waitFor(() => expect(screen.queryByTestId("full-screen-two-pane-modal")).not.toBeInTheDocument());
  });

  it("updates and deletes a response profile through the API hook", async () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage");
    mockResponseProfilesApi({ responseProfiles: testResponseProfiles });
    updateResponseProfileMock.mockResolvedValue({
      ...testResponseProfiles[1],
      name: "Site Alpha 750 kW",
      targetSummary: "750 kW target",
    });
    deleteResponseProfileMock.mockResolvedValue(undefined);

    render(
      <MemoryRouter>
        <CurtailmentSettingsPage />
      </MemoryRouter>,
    );

    fireEvent.click(within(getResponseProfileCard("Site Alpha 500 kW")).getByRole("button", { name: "Edit" }));
    expect(screen.getByText("Edit response profile")).toBeInTheDocument();
    expect(screen.getByLabelText("Profile name")).toHaveValue("Site Alpha 500 kW");
    expect(screen.getByRole("button", { name: "Curtailment mode" })).toHaveTextContent("Fixed kW reduction");
    expect(screen.getByLabelText("Fixed target reduction (kW)")).toHaveValue("500");
    expect(screen.queryByLabelText("Min duration (sec)")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Max duration (sec)")).not.toBeInTheDocument();
    expect(screen.getAllByLabelText("Batch size (miners)")).toHaveLength(2);
    expect(screen.getAllByLabelText("Batch interval (sec)")).toHaveLength(2);
    expect(screen.getByTestId("response-profile-curtail-batch-size")).toHaveValue("50");
    expect(screen.getByTestId("response-profile-curtail-batch-interval")).toHaveValue("30");
    expect(screen.getByTestId("response-profile-restore-batch-size")).toHaveValue("0");
    expect(screen.getByTestId("response-profile-restore-batch-interval")).toHaveValue("0");
    expect(screen.queryByTestId("response-profile-post-event-cooldown")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Miners\s+Select/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Sites\s+Select/ })).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Profile name"), { target: { value: "Site Alpha 750 kW" } });
    fireEvent.change(screen.getByLabelText("Fixed target reduction (kW)"), { target: { value: "750" } });
    fireEvent.change(screen.getByTestId("response-profile-curtail-batch-size"), { target: { value: "75" } });
    fireEvent.change(screen.getByTestId("response-profile-curtail-batch-interval"), { target: { value: "45" } });
    fireEvent.change(screen.getByTestId("response-profile-restore-batch-size"), { target: { value: "50" } });
    fireEvent.change(screen.getByTestId("response-profile-restore-batch-interval"), { target: { value: "120" } });
    fireEvent.click(getEnabledButton("Save profile"));

    await waitFor(() => expect(screen.queryByTestId("full-screen-two-pane-modal")).not.toBeInTheDocument());
    expect(updateResponseProfileMock).toHaveBeenCalledWith(
      "site-alpha-500-kw",
      expect.objectContaining({
        name: "Site Alpha 750 kW",
        targetKw: "750",
        curtailBatchSize: "75",
        curtailBatchIntervalSec: "45",
        restoreBatchSize: "50",
        restoreIntervalSec: "120",
        siteId: "",
      }),
    );
    expect(pushToast).toHaveBeenCalledWith({
      message: "Response profile saved",
      status: "success",
    });

    fireEvent.click(within(getResponseProfileCard("Site Alpha 500 kW")).getByRole("button", { name: "Edit" }));
    expect(screen.getByText("Edit response profile")).toBeInTheDocument();
    fireEvent.click(getEnabledButton("Delete"));

    await waitFor(() => expect(screen.queryByTestId("full-screen-two-pane-modal")).not.toBeInTheDocument());
    expect(deleteResponseProfileMock).toHaveBeenCalledWith("site-alpha-500-kw");
    expect(pushToast).toHaveBeenCalledWith({
      message: "Response profile deleted",
      status: "success",
    });
  });

  it("preserves the all-paired flag when editing and saving a profile", async () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage");
    const allPairedProfile: ResponseProfile = {
      ...testResponseProfiles[0],
      id: "all-paired-shed",
      name: "All-paired shed",
      formValues: {
        ...testResponseProfiles[0].formValues!,
        name: "All-paired shed",
        forceIncludeAllPairedMiners: true,
      },
    };
    mockResponseProfilesApi({ responseProfiles: [allPairedProfile] });
    updateResponseProfileMock.mockResolvedValue(allPairedProfile);

    render(
      <MemoryRouter>
        <CurtailmentSettingsPage />
      </MemoryRouter>,
    );

    fireEvent.click(within(getResponseProfileCard("All-paired shed")).getByRole("button", { name: "Edit" }));
    expect(screen.getByText("Edit response profile")).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "Target all paired miners" })).toBeChecked();

    fireEvent.change(screen.getByLabelText("Profile name"), { target: { value: "All-paired shed v2" } });
    fireEvent.click(getEnabledButton("Save profile"));

    await waitFor(() => expect(screen.queryByTestId("full-screen-two-pane-modal")).not.toBeInTheDocument());
    expect(updateResponseProfileMock).toHaveBeenCalledWith(
      "all-paired-shed",
      expect.objectContaining({
        name: "All-paired shed v2",
        forceIncludeAllPairedMiners: true,
      }),
    );
  });

  it("preserves site scope when saving an API response profile", async () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage" || key === "site:read");
    mockResponseProfilesApi({ responseProfiles: [siteScopedResponseProfile] });
    updateResponseProfileMock.mockResolvedValue(siteScopedResponseProfile);

    render(
      <MemoryRouter>
        <CurtailmentSettingsPage />
      </MemoryRouter>,
    );

    expect(within(getResponseProfileCard("Site scoped profile")).getByText("Site 101")).toBeVisible();

    fireEvent.click(within(getResponseProfileCard("Site scoped profile")).getByRole("button", { name: "Edit" }));
    await waitFor(() => expect(screen.getByRole("button", { name: /Sites\s+Austin, TX/ })).toBeInTheDocument());
    expect(screen.getByText("Apply to")).toBeInTheDocument();
    fireEvent.click(getEnabledButton("Save profile"));

    await waitFor(() =>
      expect(updateResponseProfileMock).toHaveBeenCalledWith(
        "site-scoped-profile",
        expect.objectContaining({
          siteId: "101",
          siteName: "Austin, TX",
        }),
      ),
    );
  });

  it("keeps the response profile modal open and shows delete failures", async () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage");
    mockResponseProfilesApi({ responseProfiles: testResponseProfiles });
    deleteResponseProfileMock.mockRejectedValue(new Error("Delete failed"));

    render(
      <MemoryRouter>
        <CurtailmentSettingsPage />
      </MemoryRouter>,
    );

    fireEvent.click(within(getResponseProfileCard("Site Alpha 500 kW")).getByRole("button", { name: "Edit" }));
    fireEvent.click(getEnabledButton("Delete"));

    await waitFor(() => expect(screen.getByTestId("curtailment-action-error")).toHaveTextContent("Delete failed"));
    expect(screen.getByText("Edit response profile")).toBeInTheDocument();
    expect(screen.getByTestId("full-screen-two-pane-modal")).toBeInTheDocument();
  });

  it("updates a response profile before running curtailment from edit mode", async () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage");
    mockResponseProfilesApi({ responseProfiles: testResponseProfiles });
    updateResponseProfileMock.mockResolvedValue({
      ...testResponseProfiles[1],
      name: "Site Alpha 750 kW",
      targetSummary: "750 kW target",
    });

    render(
      <MemoryRouter>
        <CurtailmentSettingsPage />
      </MemoryRouter>,
    );

    fireEvent.click(within(getResponseProfileCard("Site Alpha 500 kW")).getByRole("button", { name: "Edit" }));
    expect(screen.getByText("Edit response profile")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Run curtailment" })).toBeEnabled();

    fireEvent.change(screen.getByLabelText("Profile name"), { target: { value: "Site Alpha 750 kW" } });
    fireEvent.change(screen.getByLabelText("Fixed target reduction (kW)"), { target: { value: "750" } });
    fireEvent.change(screen.getByTestId("response-profile-curtail-batch-size"), { target: { value: "75" } });
    fireEvent.change(screen.getByTestId("response-profile-curtail-batch-interval"), { target: { value: "45" } });
    fireEvent.change(screen.getByTestId("response-profile-restore-batch-size"), { target: { value: "50" } });
    fireEvent.change(screen.getByTestId("response-profile-restore-batch-interval"), { target: { value: "120" } });
    fireEvent.click(getEnabledButton("Run curtailment"));
    expect(screen.getByText("Run curtailment?")).toBeInTheDocument();
    expect(
      screen.getByText(
        "This will save the profile, then trigger curtailment for miners across the fleet. Schedules stay suppressed until miners are restored.",
      ),
    ).toBeInTheDocument();
    confirmCurtailmentAction();

    await waitFor(() =>
      expect(updateResponseProfileMock).toHaveBeenCalledWith(
        "site-alpha-500-kw",
        expect.objectContaining({
          name: "Site Alpha 750 kW",
          targetKw: "750",
          curtailBatchSize: "75",
          curtailBatchIntervalSec: "45",
          restoreBatchSize: "50",
          restoreIntervalSec: "120",
          siteId: "",
        }),
      ),
    );
    await waitFor(() =>
      expect(startCurtailmentMock).toHaveBeenCalledWith(
        expect.objectContaining({
          reason: "Site Alpha 750 kW",
          targetKw: "750",
          curtailmentMode: "fixedKwReduction",
          scopeType: "wholeOrg",
          siteId: "",
          curtailBatchSize: "75",
          curtailBatchIntervalSec: "45",
          restoreBatchSize: "50",
          restoreIntervalSec: "120",
        }),
      ),
    );
    expect(createResponseProfileMock).not.toHaveBeenCalled();
    expect(mockNavigate).toHaveBeenCalledWith("/energy");
    await waitFor(() => expect(screen.queryByTestId("full-screen-two-pane-modal")).not.toBeInTheDocument());
  });

  it("renders provided response profiles as cards", () => {
    render(<CurtailmentSettingsContent initialResponseProfiles={testResponseProfiles} />);

    expect(screen.getByTestId("response-profile-card-grid")).toBeVisible();
    expect(screen.getByText("Emergency full shed")).toBeVisible();
    expect(screen.getByText("100% reduction")).toBeVisible();
    expect(within(getResponseProfileCard("Emergency full shed")).getByText("Whole fleet")).toBeVisible();
    expect(screen.getByText("Site Alpha 500 kW")).toBeVisible();
    expect(screen.getByText("500 kW target")).toBeVisible();
    expect(within(getResponseProfileCard("Site Alpha 500 kW")).getByText("Whole fleet")).toBeVisible();
    expect(within(getResponseProfileCard("Emergency full shed")).getByRole("button", { name: "Edit" })).toBeEnabled();
  });

  it("preserves targeted miner response profiles in local state", async () => {
    render(<CurtailmentSettingsContent initialResponseProfiles={[targetedMinersResponseProfile]} />);

    expect(screen.getByText("Targeted miners")).toBeVisible();
    expect(screen.getByText("650 kW target")).toBeVisible();
    expect(within(getResponseProfileCard("Targeted miners")).getByText("Whole fleet")).toBeVisible();

    fireEvent.click(within(getResponseProfileCard("Targeted miners")).getByRole("button", { name: "Edit" }));
    expect(screen.getByRole("button", { name: /Miners\s+3 miners/ })).toBeInTheDocument();
    fireEvent.click(getEnabledButton("Save profile"));

    await waitFor(() => expect(screen.queryByTestId("full-screen-two-pane-modal")).not.toBeInTheDocument());
    expect(within(getResponseProfileCard("Targeted miners")).getByText("3 miners")).toBeVisible();
  });

  it("renders provided sources with the current table styling", () => {
    render(<CurtailmentSettingsContent initialSources={testSources} />);

    expect(screen.getByText("Site Alpha MQTT")).toBeVisible();
    expect(screen.getByText("Site Beta MQTT")).toBeVisible();
    expect(screen.getByText("38 seconds ago")).toBeVisible();
    expect(screen.getByText("24 seconds ago")).toBeVisible();
    const connectedLabels = screen.getAllByText("Connected");
    expect(connectedLabels).toHaveLength(2);
    for (const connectedLabel of connectedLabels) {
      expect(connectedLabel.previousElementSibling).toHaveClass("h-2", "w-2", "rounded-full", "bg-intent-success-fill");
    }
    const waitingLabel = screen.getByText("Waiting for signal");
    expect(waitingLabel.previousElementSibling).toHaveClass("h-2", "w-2", "rounded-full", "bg-intent-warning-fill");
    const noSignalLabel = screen.getByText("No signal");
    expect(noSignalLabel.previousElementSibling).toHaveClass("h-2", "w-2", "rounded-full", "bg-intent-critical-fill");
    expect(document.querySelector(".curtailment-source-health")).not.toBeInTheDocument();
  });

  it("renders provided automations with enabled and disabled rows", () => {
    render(
      <CurtailmentSettingsContent
        initialResponseProfiles={testResponseProfiles}
        initialSources={testSources}
        initialAutomationRules={testAutomationRules}
      />,
    );

    const enabledRuleRow = screen.getByText("Site Alpha automation").closest("tr");
    const disabledRuleRow = screen.getByText("Site Beta automation").closest("tr");

    expect(enabledRuleRow).not.toBeNull();
    expect(disabledRuleRow).not.toBeNull();
    const enabledRule = enabledRuleRow as HTMLTableRowElement;
    const disabledRule = disabledRuleRow as HTMLTableRowElement;
    expect(within(enabledRule).getByText("Site Alpha MQTT grid signal changes to 0")).toBeVisible();
    expect(within(disabledRule).getByText("Site Beta MQTT grid signal changes to 0")).toBeVisible();
    expect(within(enabledRule).getByText("Emergency full shed")).toBeVisible();
    expect(within(disabledRule).getByText("Site Alpha 500 kW")).toBeVisible();
    expect(within(enabledRule).getByRole("checkbox")).toBeChecked();
    expect(within(disabledRule).getByRole("checkbox")).not.toBeChecked();
  });

  it("disables infrastructure fan edits for a response profile used by an automation", () => {
    render(
      <CurtailmentSettingsContent
        initialResponseProfiles={testResponseProfiles}
        initialAutomationRules={testAutomationRules}
      />,
    );

    fireEvent.click(screen.getByTestId("response-profile-edit-emergency-full-shed"));

    expect(screen.getByRole("button", { name: /Infrastructure\s+Select/ })).toBeDisabled();
    expect(
      screen.getByText(
        "An automation uses this profile. Update or delete the automation before changing infrastructure fans.",
      ),
    ).toBeInTheDocument();
  });

  it("creates a response profile in local state", async () => {
    render(<CurtailmentSettingsContent initialSources={testSources} />);

    fireEvent.click(screen.getByRole("button", { name: "Create profile" }));

    expect(screen.getByText("Create response profile")).toBeInTheDocument();
    expect(screen.getByLabelText("Profile name")).toHaveValue("");
    expect(screen.getByRole("button", { name: "Curtailment mode" })).toHaveTextContent("Full shutdown");
    expect(screen.queryByLabelText("Fixed target reduction (kW)")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Miners\s+Select/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Sites\s+Select/ })).toBeInTheDocument();
    expect(screen.queryByLabelText("Min duration (sec)")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Max duration (sec)")).not.toBeInTheDocument();
    expect(screen.queryByRole("checkbox", { name: "Include miners in maintenance" })).not.toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "Target all paired miners" })).not.toBeChecked();
    expect(screen.getAllByLabelText("Batch size (miners)")).toHaveLength(2);
    expect(screen.getAllByLabelText("Batch interval (sec)")).toHaveLength(2);
    expect(screen.getByTestId("response-profile-curtail-batch-size")).toHaveValue("");
    expect(screen.getByTestId("response-profile-curtail-batch-interval")).toHaveValue("");
    expect(screen.getByTestId("response-profile-restore-batch-size")).toHaveValue("");
    expect(screen.getByTestId("response-profile-restore-batch-interval")).toHaveValue("");

    const saveButtons = screen.getAllByRole("button", { name: "Save profile" });
    expect(saveButtons.every((button) => button instanceof HTMLButtonElement && !button.disabled)).toBe(true);

    fireEvent.click(getEnabledButton("Save profile"));
    await waitFor(() => expect(screen.getByText("Enter a profile name.")).toBeVisible());

    fireEvent.change(screen.getByLabelText("Profile name"), { target: { value: "Emergency full shed" } });
    fireEvent.change(screen.getByTestId("response-profile-curtail-batch-size"), { target: { value: "25" } });
    fireEvent.change(screen.getByTestId("response-profile-curtail-batch-interval"), { target: { value: "60" } });
    fireEvent.change(screen.getByTestId("response-profile-restore-batch-size"), { target: { value: "10" } });
    fireEvent.change(screen.getByTestId("response-profile-restore-batch-interval"), { target: { value: "120" } });

    fireEvent.click(getEnabledButton("Save profile"));

    await waitFor(() => expect(screen.queryByTestId("full-screen-two-pane-modal")).not.toBeInTheDocument());
    expect(screen.getByText("Emergency full shed")).toBeVisible();
    expect(screen.getByText("100% reduction")).toBeVisible();
    expect(screen.getByText("Whole fleet")).toBeVisible();
    expect(within(getResponseProfileCard("Emergency full shed")).getByRole("button", { name: "Edit" })).toBeEnabled();
  });

  it("edits and deletes a response profile in local state", async () => {
    render(<CurtailmentSettingsContent initialResponseProfiles={testResponseProfiles} initialSources={testSources} />);

    fireEvent.click(within(getResponseProfileCard("Site Alpha 500 kW")).getByRole("button", { name: "Edit" }));

    expect(screen.getByText("Edit response profile")).toBeInTheDocument();
    expect(screen.getByLabelText("Profile name")).toHaveValue("Site Alpha 500 kW");
    expect(screen.getByRole("button", { name: "Curtailment mode" })).toHaveTextContent("Fixed kW reduction");
    expect(screen.getByLabelText("Fixed target reduction (kW)")).toHaveValue("500");
    expect(screen.queryByLabelText("Min duration (sec)")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Max duration (sec)")).not.toBeInTheDocument();
    expect(screen.getByTestId("response-profile-curtail-batch-size")).toHaveValue("50");
    expect(screen.getByTestId("response-profile-curtail-batch-interval")).toHaveValue("30");
    expect(screen.getByTestId("response-profile-restore-batch-size")).toHaveValue("0");
    expect(screen.getByTestId("response-profile-restore-batch-interval")).toHaveValue("0");
    expect(screen.getByRole("button", { name: /Miners\s+Select/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Sites\s+Select/ })).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Profile name"), { target: { value: "Site Alpha 750 kW" } });
    fireEvent.change(screen.getByLabelText("Fixed target reduction (kW)"), { target: { value: "750" } });
    fireEvent.change(screen.getByTestId("response-profile-curtail-batch-size"), { target: { value: "75" } });
    fireEvent.change(screen.getByTestId("response-profile-curtail-batch-interval"), { target: { value: "45" } });
    fireEvent.change(screen.getByTestId("response-profile-restore-batch-size"), { target: { value: "50" } });
    fireEvent.change(screen.getByTestId("response-profile-restore-batch-interval"), { target: { value: "120" } });
    fireEvent.click(getEnabledButton("Save profile"));

    await waitFor(() => expect(screen.queryByTestId("full-screen-two-pane-modal")).not.toBeInTheDocument());
    expect(screen.getByText("Site Alpha 750 kW")).toBeVisible();
    expect(screen.getByText("750 kW target")).toBeVisible();
    expect(screen.queryByText("Site Alpha 500 kW")).not.toBeInTheDocument();

    fireEvent.click(within(getResponseProfileCard("Site Alpha 750 kW")).getByRole("button", { name: "Edit" }));
    fireEvent.click(getEnabledButton("Delete"));

    await waitFor(() => expect(screen.queryByTestId("full-screen-two-pane-modal")).not.toBeInTheDocument());
    expect(screen.queryByText("Site Alpha 750 kW")).not.toBeInTheDocument();
  });

  it("opens the source dialog and closes it from Save without API props", async () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage");

    render(
      <MemoryRouter>
        <CurtailmentSettingsContent initialSources={testSources} />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Add source" }));

    expect(screen.getByTestId("curtailment-source-modal")).toBeInTheDocument();
    expect(screen.getByText("MaestroOS MQTT brokers that publish curtailment signals.")).toBeInTheDocument();
    expect(screen.getByText("Configuration name")).toBeInTheDocument();
    for (const fieldLabel of [
      "Configuration name",
      "Broker host 1",
      "Broker host 2",
      "Port",
      "Topic",
      "Username",
      "Password",
    ]) {
      expect((screen.getByLabelText(fieldLabel) as HTMLInputElement).value).toBe("");
    }
    expect(screen.getByLabelText("No signal timeout")).toHaveValue(240);
    expect(screen.getByLabelText("Integration")).toHaveValue("MaestroOS");
    expect(screen.getByLabelText("Integration")).toBeDisabled();
    const portTooltip = screen.getByText("Default MQTT port for MaestroOS is 1883.").parentElement;
    const topicTooltip = screen.getByText(
      "The MQTT topic to subscribe to on MaestroOS for curtailment signals.",
    ).parentElement;
    const timeoutTooltip = screen.getByText(
      "When no MQTT signal is received for this duration, the source is treated as OFF.",
    ).parentElement;
    expect(portTooltip).toHaveClass("z-50", "w-72", "left-[16px]");
    expect(portTooltip?.parentElement?.parentElement).toHaveClass("z-50");
    expect(topicTooltip).toHaveClass("w-72");
    expect(timeoutTooltip).toHaveClass("w-72");
    expect(screen.getAllByText("Port")).toHaveLength(1);
    expect(screen.getAllByText("Topic")).toHaveLength(1);
    expect(screen.getAllByText("No signal timeout")).toHaveLength(1);
    expect(screen.queryByText(/TLS/)).not.toBeInTheDocument();

    const testConnectionButton = screen.getByRole("button", { name: "Test connection" });
    const saveButton = screen.getByRole("button", { name: "Save" });
    expect(testConnectionButton).toBeDisabled();
    expect(saveButton).toBeEnabled();
    expect(testConnectionButton.compareDocumentPosition(saveButton)).toBe(Node.DOCUMENT_POSITION_FOLLOWING);

    fireEvent.click(saveButton);

    expect(screen.getByTestId("curtailment-source-modal")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText("Enter a configuration name.")).toBeVisible());
    expect(screen.getByText("Enter broker host 1.")).toBeVisible();
    expect(screen.getByText("Enter broker host 2.")).toBeVisible();
    expect(screen.getByText("Enter a port.")).toBeVisible();
    expect(screen.getByText("Enter a topic.")).toBeVisible();
    expect(screen.getByText("Enter a username.")).toBeVisible();
    expect(screen.getByText("Enter a password.")).toBeVisible();

    fillSourceForm();

    expect(saveButton).toBeEnabled();

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(screen.queryByTestId("curtailment-source-modal")).not.toBeInTheDocument());
  });

  it("creates a source through the API hook from the routed page", async () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage");
    createSourceMock.mockResolvedValue(apiSources[0]);

    render(
      <MemoryRouter>
        <CurtailmentSettingsPage />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Add source" }));
    fillSourceForm();

    fireEvent.keyDown(screen.getByLabelText("Password"), { key: "Enter", code: "Enter" });

    await waitFor(() => expect(createSourceMock).toHaveBeenCalledWith(testSourceFormValues));
    await waitFor(() => expect(screen.queryByTestId("curtailment-source-modal")).not.toBeInTheDocument());
    expect(pushToast).toHaveBeenCalledWith({
      message: "Source added",
      status: "success",
    });
  });

  it("tests a source connection through the API hook from the routed page", async () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage");
    testConnectionMock.mockResolvedValue(undefined);

    render(
      <MemoryRouter>
        <CurtailmentSettingsPage />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Add source" }));
    fillSourceForm();
    fireEvent.click(screen.getByRole("button", { name: "Test connection" }));

    await waitFor(() => expect(testConnectionMock).toHaveBeenCalledWith(testSourceFormValues));
    expect(screen.getByTestId("curtailment-source-connected-callout")).toHaveClass("max-h-96");
    expect(screen.getByTestId("curtailment-source-modal")).toBeInTheDocument();
  });

  it("shows a source connection failure callout when the test fails", async () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage");
    testConnectionMock.mockRejectedValue(new Error("failed"));

    render(
      <MemoryRouter>
        <CurtailmentSettingsPage />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Add source" }));
    fillSourceForm();
    fireEvent.click(screen.getByRole("button", { name: "Test connection" }));

    await waitFor(() => expect(testConnectionMock).toHaveBeenCalledWith(testSourceFormValues));
    expect(screen.getByTestId("curtailment-source-not-connected-callout")).toHaveClass("max-h-96");
    expect(
      screen.getByText("We couldn't connect with your source. Review your source details and try again."),
    ).toBeInTheDocument();
  });

  it("opens the edit source dialog with source details when a source row is clicked", () => {
    render(
      <MemoryRouter>
        <CurtailmentSettingsContent initialSources={testSources} />
      </MemoryRouter>,
    );

    fireEvent.click(getSourceRow("Site Alpha MQTT"));

    expect(screen.getByText("Edit source")).toBeInTheDocument();
    expect(screen.getByLabelText("Configuration name")).toHaveValue("Site Alpha MQTT");
    expect(screen.getByLabelText("Broker host 1")).toHaveValue("site-alpha-primary.broker.test");
    expect(screen.getByLabelText("Broker host 2")).toHaveValue("site-alpha-secondary.broker.test");
    expect(screen.getByLabelText("Port")).toHaveValue(11883);
    expect(screen.getByLabelText("Topic")).toHaveValue("curtailment/site-alpha/target");
    expect(screen.getByLabelText("Username")).toHaveValue("curtailment-alpha");
    expect(screen.getByLabelText("Password")).toHaveValue("");
    expect(screen.getByLabelText("No signal timeout")).toHaveValue(240);

    const testConnectionButton = screen.getByRole("button", { name: "Test connection" });
    const deleteButton = screen.getByRole("button", { name: "Delete" });
    const saveButton = screen.getByRole("button", { name: "Save" });
    expect(saveButton).toBeEnabled();
    expect(deleteButton.compareDocumentPosition(testConnectionButton)).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
    expect(testConnectionButton.compareDocumentPosition(saveButton)).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
  });

  it("hides the password eye for the saved-password placeholder until the password field is focused", async () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage");
    updateSourceMock.mockResolvedValue(apiSources[0]);
    mockSourcesApi({ sources: apiSources, updateSource: updateSourceMock });

    render(
      <MemoryRouter>
        <CurtailmentSettingsPage />
      </MemoryRouter>,
    );

    fireEvent.click(getSourceRow("Site Alpha MQTT"));

    const passwordInput = screen.getByLabelText("Password");
    expect(passwordInput).toHaveValue("......");
    expect(passwordInput).toHaveAttribute("type", "password");
    expect(screen.queryByTestId("eye-icon")).not.toBeInTheDocument();

    fireEvent.focus(passwordInput);

    expect(passwordInput).toHaveValue("");
    expect(screen.getByTestId("eye-icon")).toBeInTheDocument();

    fireEvent.change(passwordInput, { target: { value: "updated-secret" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(updateSourceMock).toHaveBeenCalledWith("11", {
        name: "Site Alpha MQTT",
        brokerPrimaryHost: "site-alpha-primary.broker.test",
        brokerSecondaryHost: "site-alpha-secondary.broker.test",
        brokerPort: "11883",
        topic: "curtailment/site-alpha/target",
        username: "curtailment-alpha",
        password: "updated-secret",
        stalenessThresholdSec: "240",
      }),
    );
  });

  it("clears the saved-password placeholder when testing an edited source requires a password", async () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage");
    mockSourcesApi({ sources: apiSources });

    render(
      <MemoryRouter>
        <CurtailmentSettingsPage />
      </MemoryRouter>,
    );

    fireEvent.click(getSourceRow("Site Alpha MQTT"));

    const passwordInput = screen.getByLabelText("Password");
    expect(passwordInput).toHaveValue("......");

    fireEvent.click(screen.getByRole("button", { name: "Test connection" }));

    await waitFor(() => expect(screen.getByText("Enter a password.")).toBeVisible());
    expect(passwordInput).toHaveValue("");
    expect(testConnectionMock).not.toHaveBeenCalled();
  });

  it("clears the saved-password placeholder when saving an edited source requires a password", async () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage");
    mockSourcesApi({ sources: apiSources });

    render(
      <MemoryRouter>
        <CurtailmentSettingsPage />
      </MemoryRouter>,
    );

    fireEvent.click(getSourceRow("Site Alpha MQTT"));

    const passwordInput = screen.getByLabelText("Password");
    expect(passwordInput).toHaveValue("......");

    fireEvent.change(screen.getByLabelText("Username"), { target: { value: "updated-alpha" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(screen.getByText("Enter a password.")).toBeVisible());
    expect(passwordInput).toHaveValue("");
    expect(updateSourceMock).not.toHaveBeenCalled();
  });

  it("updates a source through the API hook from the routed page", async () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage");
    updateSourceMock.mockResolvedValue({ ...apiSources[0], name: "Site Alpha MQTT updated" });
    mockSourcesApi({ sources: apiSources, updateSource: updateSourceMock });

    render(
      <MemoryRouter>
        <CurtailmentSettingsPage />
      </MemoryRouter>,
    );

    fireEvent.click(getSourceRow("Site Alpha MQTT"));
    fireEvent.change(screen.getByLabelText("Configuration name"), { target: { value: "Site Alpha MQTT updated" } });
    fireEvent.change(screen.getByLabelText("No signal timeout"), { target: { value: "300" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(updateSourceMock).toHaveBeenCalledWith("11", {
        name: "Site Alpha MQTT updated",
        brokerPrimaryHost: "site-alpha-primary.broker.test",
        brokerSecondaryHost: "site-alpha-secondary.broker.test",
        brokerPort: "11883",
        topic: "curtailment/site-alpha/target",
        username: "curtailment-alpha",
        password: "",
        stalenessThresholdSec: "300",
      }),
    );
    await waitFor(() => expect(screen.queryByTestId("curtailment-source-modal")).not.toBeInTheDocument());
    expect(createSourceMock).not.toHaveBeenCalled();
    expect(pushToast).toHaveBeenCalledWith({
      message: "Source saved",
      status: "success",
    });
  });

  it("rejects oversized source no-signal timeout values", async () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage");
    mockSourcesApi({ sources: apiSources, updateSource: updateSourceMock });

    render(
      <MemoryRouter>
        <CurtailmentSettingsPage />
      </MemoryRouter>,
    );

    fireEvent.click(getSourceRow("Site Alpha MQTT"));
    fireEvent.change(screen.getByLabelText("No signal timeout"), { target: { value: "86401" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(screen.getByText("Enter timeout of 86,400 seconds or less.")).toBeVisible());
    expect(updateSourceMock).not.toHaveBeenCalled();
  });

  it("deletes a source through the API hook from the routed page", async () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage");
    deleteSourceMock.mockResolvedValue(undefined);
    mockSourcesApi({ sources: apiSources, deleteSource: deleteSourceMock });

    render(
      <MemoryRouter>
        <CurtailmentSettingsPage />
      </MemoryRouter>,
    );

    fireEvent.click(getSourceRow("Site Alpha MQTT"));
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));

    await waitFor(() => expect(deleteSourceMock).toHaveBeenCalledWith("11"));
    await waitFor(() => expect(screen.queryByTestId("curtailment-source-modal")).not.toBeInTheDocument());
    expect(pushToast).toHaveBeenCalledWith({
      message: "Source deleted",
      status: "success",
    });
  });

  it("toggles the sources info popover", () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage");

    render(
      <MemoryRouter>
        <CurtailmentSettingsPage />
      </MemoryRouter>,
    );

    const infoButton = screen.getByRole("button", { name: "About sources" });

    expect(infoButton).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByTestId("curtailment-sources-info-popover")).not.toBeInTheDocument();

    fireEvent.click(infoButton);

    expect(infoButton).toHaveAttribute("aria-expanded", "true");
    const popover = screen.getByTestId("curtailment-sources-info-popover");
    expect(popover).toHaveTextContent("MaestroOS MQTT brokers that publish curtailment signals.");

    fireEvent.click(infoButton);

    expect(infoButton).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByTestId("curtailment-sources-info-popover")).not.toBeInTheDocument();
  });

  it("keeps source enablement as local state without API props", () => {
    render(
      <MemoryRouter>
        <CurtailmentSettingsContent initialSources={testSources} />
      </MemoryRouter>,
    );

    const alphaSwitch = within(getSourceRow("Site Alpha MQTT")).getByRole("checkbox");
    expect(alphaSwitch).toBeChecked();

    fireEvent.click(alphaSwitch);

    expect(alphaSwitch).not.toBeChecked();
  });

  it("persists source enablement through the API hook on the routed page", () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage");
    setSourceEnabledMock.mockResolvedValue({ ...apiSources[0], enabled: false });
    mockSourcesApi({ sources: apiSources, setSourceEnabled: setSourceEnabledMock });

    render(
      <MemoryRouter>
        <CurtailmentSettingsPage />
      </MemoryRouter>,
    );

    fireEvent.click(within(getSourceRow("Site Alpha MQTT")).getByRole("checkbox"));

    expect(setSourceEnabledMock).toHaveBeenCalledWith("11", false);
  });

  it("shows a toast when source enablement fails", async () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage");
    setSourceEnabledMock.mockRejectedValue(new Error("Toggle failed"));
    mockSourcesApi({ sources: apiSources, setSourceEnabled: setSourceEnabledMock });

    render(
      <MemoryRouter>
        <CurtailmentSettingsPage />
      </MemoryRouter>,
    );

    fireEvent.click(within(getSourceRow("Site Alpha MQTT")).getByRole("checkbox"));

    expect(setSourceEnabledMock).toHaveBeenCalledWith("11", false);
    await waitFor(() =>
      expect(pushToast).toHaveBeenCalledWith({
        message: "Toggle failed",
        status: "error",
      }),
    );
  });

  it("creates automation rules through the API hook on the routed page", async () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage");
    createAutomationRuleMock.mockResolvedValue(apiAutomationRules[0]);
    mockSourcesApi({ sources: apiSources });
    mockResponseProfilesApi({ responseProfiles: apiResponseProfiles });

    render(
      <MemoryRouter>
        <CurtailmentSettingsPage />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Create automation" }));
    fireEvent.change(screen.getByLabelText("Rule name"), { target: { value: "Site Alpha automation" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(createAutomationRuleMock).toHaveBeenCalledWith({
        name: "Site Alpha automation",
        sourceId: "11",
        responseProfileId: "21",
      }),
    );
    await waitFor(() =>
      expect(pushToast).toHaveBeenCalledWith({
        message: "Automation added",
        status: "success",
      }),
    );
  });

  it("redirects callers without curtailment management permission", () => {
    vi.mocked(useHasPermission).mockReturnValue(false);

    render(
      <MemoryRouter>
        <CurtailmentSettingsPage />
      </MemoryRouter>,
    );

    expect(useHasPermission).toHaveBeenCalledWith("curtailment:manage");
    expect(useCurtailmentResponseProfiles).toHaveBeenCalledWith(false, { siteNameById: undefined });
    expect(useMqttCurtailmentSources).toHaveBeenCalledWith(false);
    expect(useCurtailmentAutomationRules).toHaveBeenCalledWith(false);
    expect(screen.queryByTestId("settings-curtailment-page")).not.toBeInTheDocument();
  });
});
