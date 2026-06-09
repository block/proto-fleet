import { MemoryRouter } from "react-router-dom";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import useMqttCurtailmentSources from "@/protoFleet/api/useMqttCurtailmentSources";
import CurtailmentSettingsPage, {
  CurtailmentSettingsContent,
} from "@/protoFleet/features/settings/components/Curtailment";
import type { CurtailmentSource } from "@/protoFleet/features/settings/components/Curtailment/types";
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
    id: "kati-maestro",
    name: "Kati MaestroOS",
    triggerType: "MQTT",
    site: "Kati",
    brokerHosts: ["10.155.0.3", "10.155.0.4"],
    port: 1883,
    topic: "maestro/target",
    protocol: "MQTT 3.1.1",
    qos: 1,
    username: "soluna-kati",
    scope: "Kati",
    curtailmentMode: "Curtail entire site",
    lastTarget: "0",
    lastSeen: "38 seconds ago",
    health: "connected",
    enabled: true,
  },
  {
    id: "dorothy-2-maestro",
    name: "Dorothy 2 MaestroOS",
    triggerType: "MQTT",
    site: "Dorothy 2",
    brokerHosts: ["10.144.0.3", "10.144.0.4"],
    port: 1883,
    topic: "maestro/target",
    protocol: "MQTT 3.1.1",
    qos: 1,
    username: "soluna-dorothy",
    scope: "Dorothy 2",
    curtailmentMode: "Curtail entire site",
    lastTarget: "100",
    lastSeen: "24 seconds ago",
    health: "connected",
    enabled: true,
  },
];

const apiSources: CurtailmentSource[] = [
  {
    ...testSources[0],
    id: "11",
  },
];

const createSourceMock = vi.fn();
const setSourceEnabledMock = vi.fn();

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
    setSourceEnabled: setSourceEnabledMock,
    ...overrides,
  });
};

describe("CurtailmentSettingsPage", () => {
  beforeEach(() => {
    vi.mocked(useHasPermission).mockReset();
    vi.mocked(useMqttCurtailmentSources).mockReset();
    vi.mocked(pushToast).mockReset();
    createSourceMock.mockReset();
    setSourceEnabledMock.mockReset();
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
    expect(screen.queryByText("Kati MaestroOS")).not.toBeInTheDocument();
    expect(screen.queryByText("Dorothy 2 MaestroOS")).not.toBeInTheDocument();
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

    expect(screen.getByText("Kati MaestroOS")).toBeVisible();
    expect(screen.getByText("38 seconds ago")).toBeVisible();
  });

  it("renders provided sources with the current table styling", () => {
    render(<CurtailmentSettingsContent initialSources={testSources} />);

    expect(screen.getByText("Kati MaestroOS")).toBeVisible();
    expect(screen.getByText("Dorothy 2 MaestroOS")).toBeVisible();
    expect(screen.getByText("38 seconds ago")).toBeVisible();
    expect(screen.getByText("24 seconds ago")).toBeVisible();
    const connectedLabels = screen.getAllByText("Connected");
    expect(connectedLabels).toHaveLength(2);
    for (const connectedLabel of connectedLabels) {
      expect(connectedLabel.previousElementSibling).toHaveClass("h-2", "w-2", "rounded-full", "bg-intent-success-fill");
    }
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

    fireEvent.change(screen.getByLabelText("Configuration name"), { target: { value: "Kati MaestroOS" } });
    fireEvent.change(screen.getByLabelText("Broker host 1"), { target: { value: "10.155.0.3" } });
    fireEvent.change(screen.getByLabelText("Broker host 2"), { target: { value: "10.155.0.4" } });
    fireEvent.change(screen.getByLabelText("Port"), { target: { value: "1883" } });
    fireEvent.change(screen.getByLabelText("Topic"), { target: { value: "maestro/target" } });
    fireEvent.change(screen.getByLabelText("Username"), { target: { value: "soluna-kati" } });
    fireEvent.change(screen.getByLabelText("Password"), { target: { value: "secret" } });

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
    fireEvent.change(screen.getByLabelText("Configuration name"), { target: { value: "Kati MaestroOS" } });
    fireEvent.change(screen.getByLabelText("Broker host 1"), { target: { value: "10.155.0.3" } });
    fireEvent.change(screen.getByLabelText("Broker host 2"), { target: { value: "10.155.0.4" } });
    fireEvent.change(screen.getByLabelText("Port"), { target: { value: "1883" } });
    fireEvent.change(screen.getByLabelText("Topic"), { target: { value: "maestro/target" } });
    fireEvent.change(screen.getByLabelText("Username"), { target: { value: "soluna-kati" } });
    fireEvent.change(screen.getByLabelText("Password"), { target: { value: "secret" } });

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(createSourceMock).toHaveBeenCalledWith({
        name: "Kati MaestroOS",
        brokerPrimaryHost: "10.155.0.3",
        brokerSecondaryHost: "10.155.0.4",
        brokerPort: "1883",
        topic: "maestro/target",
        username: "soluna-kati",
        password: "secret",
      }),
    );
    await waitFor(() => expect(screen.queryByTestId("curtailment-source-modal")).not.toBeInTheDocument());
    expect(pushToast).toHaveBeenCalledWith({
      message: "Source added",
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

    const katiRow = screen.getByText("Kati MaestroOS").closest("tr");
    expect(katiRow).not.toBeNull();

    const katiSwitch = within(katiRow as HTMLTableRowElement).getByRole("checkbox");
    expect(katiSwitch).toBeChecked();

    fireEvent.click(katiSwitch);

    expect(katiSwitch).not.toBeChecked();
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

    const katiRow = screen.getByText("Kati MaestroOS").closest("tr");
    expect(katiRow).not.toBeNull();

    fireEvent.click(within(katiRow as HTMLTableRowElement).getByRole("checkbox"));

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
