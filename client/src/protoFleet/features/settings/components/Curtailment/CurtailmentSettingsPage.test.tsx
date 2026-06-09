import { MemoryRouter } from "react-router-dom";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import useMqttCurtailmentSources from "@/protoFleet/api/useMqttCurtailmentSources";
import CurtailmentSettingsPage, {
  CurtailmentSettingsContent,
} from "@/protoFleet/features/settings/components/Curtailment";
import type {
  CurtailmentSource,
  CurtailmentSourceFormValues,
} from "@/protoFleet/features/settings/components/Curtailment/types";
import { useHasPermission } from "@/protoFleet/store";
import { pushToast } from "@/shared/features/toaster";

vi.mock("@/protoFleet/store", () => ({
  useHasPermission: vi.fn(),
}));

vi.mock("@/protoFleet/api/useMqttCurtailmentSources", () => ({
  default: vi.fn(),
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
    site: "Site Alpha",
    brokerHosts: ["site-alpha-primary.broker.test", "site-alpha-secondary.broker.test"],
    port: 11883,
    topic: "curtailment/site-alpha/target",
    protocol: "MQTT 3.1.1",
    qos: 1,
    username: "curtailment-alpha",
    scope: "Site Alpha",
    curtailmentMode: "Curtail entire site",
    lastTarget: "0",
    lastSeen: "38 seconds ago",
    health: "connected",
    enabled: true,
  },
  {
    id: "site-beta-mqtt",
    name: "Site Beta MQTT",
    triggerType: "MQTT",
    site: "Site Beta",
    brokerHosts: ["site-beta-primary.broker.test", "site-beta-secondary.broker.test"],
    port: 11884,
    topic: "curtailment/site-beta/target",
    protocol: "MQTT 3.1.1",
    qos: 1,
    username: "curtailment-beta",
    scope: "Site Beta",
    curtailmentMode: "Curtail entire site",
    lastTarget: "100",
    lastSeen: "24 seconds ago",
    health: "connected",
    enabled: true,
  },
  {
    id: "site-gamma-mqtt",
    name: "Site Gamma MQTT",
    triggerType: "MQTT",
    site: "Site Gamma",
    brokerHosts: ["site-gamma-primary.broker.test", "site-gamma-secondary.broker.test"],
    port: 11885,
    topic: "curtailment/site-gamma/target",
    protocol: "MQTT 3.1.1",
    qos: 1,
    username: "curtailment-gamma",
    scope: "Site Gamma",
    curtailmentMode: "Curtail entire site",
    lastTarget: "-",
    lastSeen: "-",
    health: "waitingForSignal",
    enabled: true,
  },
  {
    id: "site-delta-mqtt",
    name: "Site Delta MQTT",
    triggerType: "MQTT",
    site: "Site Delta",
    brokerHosts: ["site-delta-primary.broker.test", "site-delta-secondary.broker.test"],
    port: 11886,
    topic: "curtailment/site-delta/target",
    protocol: "MQTT 3.1.1",
    qos: 1,
    username: "curtailment-delta",
    scope: "Site Delta",
    curtailmentMode: "Curtail entire site",
    lastTarget: "-",
    lastSeen: "12 minutes ago",
    health: "noSignal",
    enabled: true,
  },
];

const apiSources: CurtailmentSource[] = [
  {
    ...testSources[0],
    id: "11",
    hasPassword: true,
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
};

const createSourceMock = vi.fn();
const updateSourceMock = vi.fn();
const setSourceEnabledMock = vi.fn();
const deleteSourceMock = vi.fn();

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
    setSourceEnabled: setSourceEnabledMock,
    deleteSource: deleteSourceMock,
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
}

function getSourceRow(sourceName: string): HTMLTableRowElement {
  const row = screen.getByText(sourceName).closest("tr");
  expect(row).not.toBeNull();
  return row as HTMLTableRowElement;
}

describe("CurtailmentSettingsPage", () => {
  beforeEach(() => {
    vi.mocked(useHasPermission).mockReset();
    vi.mocked(useMqttCurtailmentSources).mockReset();
    vi.mocked(pushToast).mockReset();
    createSourceMock.mockReset();
    updateSourceMock.mockReset();
    setSourceEnabledMock.mockReset();
    deleteSourceMock.mockReset();
    mockSourcesApi();
  });

  it("renders the curtailment header and sources table", () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage");

    render(
      <MemoryRouter>
        <CurtailmentSettingsPage />
      </MemoryRouter>,
    );

    expect(useHasPermission).toHaveBeenCalledWith("curtailment:manage");
    expect(useMqttCurtailmentSources).toHaveBeenCalledWith(true);
    expect(screen.getByTestId("settings-curtailment-page")).toBeVisible();
    expect(screen.getByText("Curtailment")).toBeVisible();
    expect(
      screen.getByText(
        "Configure response profiles, manage external signal sources, and define automations that trigger curtailment.",
      ),
    ).toBeVisible();
    expect(screen.getByText("Sources")).toBeVisible();
    expect(screen.getByRole("button", { name: "About sources" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Add source" })).toBeEnabled();
    expect(document.querySelector(".curtailment-section-header__icon")).not.toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Name" }).closest("table")?.className).toContain(
      "[&_thead_th]:text-text-primary-50",
    );

    for (const columnName of ["Name", "Last signal", "Updated", "Connection", "Enabled"]) {
      expect(screen.getByRole("columnheader", { name: columnName })).toBeInTheDocument();
    }
    expect(screen.queryByRole("columnheader", { name: "Last target" })).not.toBeInTheDocument();
    expect(screen.queryByRole("columnheader", { name: "Type" })).not.toBeInTheDocument();
    expect(screen.queryByRole("columnheader", { name: "Broker hosts" })).not.toBeInTheDocument();
    expect(screen.queryByText("Site Alpha MQTT")).not.toBeInTheDocument();
    expect(screen.queryByText("Site Beta MQTT")).not.toBeInTheDocument();
    expect(screen.getByTestId("list-empty-row")).toBeInTheDocument();
    expect(screen.getByText("No sources configured")).toBeVisible();
    expect(screen.getByText("Add a source to receive curtailment signals via MQTT.")).toBeVisible();
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

  it("opens the source dialog and closes it from Save without API props", async () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:manage");

    render(
      <MemoryRouter>
        <CurtailmentSettingsContent initialSources={testSources} />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Add source" }));

    expect(screen.getByTestId("curtailment-source-modal")).toBeInTheDocument();
    expect(screen.getByText("External systems that send curtailment signals via MQTT.")).toBeInTheDocument();
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
    expect(screen.getByLabelText("Source type")).toHaveValue("MQTT");
    expect(screen.getByLabelText("Source type")).toBeDisabled();
    const portTooltip = screen.getByText("Default MQTT port is 1883.").parentElement;
    const topicTooltip = screen.getByText("The MQTT topic to subscribe to for curtailment signals.").parentElement;
    expect(portTooltip).toHaveClass("z-50", "w-72", "left-[16px]");
    expect(portTooltip?.parentElement?.parentElement).toHaveClass("z-50");
    expect(topicTooltip).toHaveClass("w-72");
    expect(screen.getAllByText("Port")).toHaveLength(1);
    expect(screen.getAllByText("Topic")).toHaveLength(1);
    expect(screen.queryByText(/TLS/)).not.toBeInTheDocument();

    const testConnectionButton = screen.getByRole("button", { name: "Test connection" });
    const saveButton = screen.getByRole("button", { name: "Save" });
    expect(testConnectionButton).toBeDisabled();
    expect(saveButton).toBeDisabled();
    expect(testConnectionButton.compareDocumentPosition(saveButton)).toBe(Node.DOCUMENT_POSITION_FOLLOWING);

    fireEvent.click(testConnectionButton);

    expect(screen.getByTestId("curtailment-source-modal")).toBeInTheDocument();

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

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(createSourceMock).toHaveBeenCalledWith(testSourceFormValues));
    await waitFor(() => expect(screen.queryByTestId("curtailment-source-modal")).not.toBeInTheDocument());
    expect(pushToast).toHaveBeenCalledWith({
      message: "Source added",
      status: "success",
    });
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

    const testConnectionButton = screen.getByRole("button", { name: "Test connection" });
    const deleteButton = screen.getByRole("button", { name: "Delete" });
    const saveButton = screen.getByRole("button", { name: "Save" });
    expect(saveButton).toBeEnabled();
    expect(testConnectionButton.compareDocumentPosition(deleteButton)).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
    expect(deleteButton.compareDocumentPosition(saveButton)).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
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
      }),
    );
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
      }),
    );
    await waitFor(() => expect(screen.queryByTestId("curtailment-source-modal")).not.toBeInTheDocument());
    expect(createSourceMock).not.toHaveBeenCalled();
    expect(pushToast).toHaveBeenCalledWith({
      message: "Source saved",
      status: "success",
    });
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
    expect(popover).toHaveTextContent("External systems that send curtailment signals via MQTT.");

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

  it("redirects callers without curtailment management permission", () => {
    vi.mocked(useHasPermission).mockReturnValue(false);

    render(
      <MemoryRouter>
        <CurtailmentSettingsPage />
      </MemoryRouter>,
    );

    expect(useHasPermission).toHaveBeenCalledWith("curtailment:manage");
    expect(useMqttCurtailmentSources).toHaveBeenCalledWith(false);
    expect(screen.queryByTestId("settings-curtailment-page")).not.toBeInTheDocument();
  });
});
