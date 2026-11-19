import { createMemoryRouter, RouterProvider, useSearchParams } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import ContainerHomeLayout from "./ContainerHomeLayout";
import ContainerHomePage from "./ContainerHomePage";
import { containerHistoryKey, type ContainerHistoryRequest } from "@/protoOS/api/adapters/containerHistory";
import { MdkV2Adapter, type MdkV2Client } from "@/protoOS/api/adapters/mdkV2/MdkV2Adapter";
import { toCanonicalContainer } from "@/protoOS/api/adapters/mdkV2/normalize";
import type { ContainerSettings } from "@/protoOS/api/adapters/types";
import { containerRoutePrefetch, importContainerHome, singleMinerRoutePrefetch } from "@/protoOS/routePrefetch";
import useMinerStore from "@/protoOS/store/useMinerStore";

const mocks = vi.hoisted(() => ({
  pushToast: vi.fn(),
  adapter: {
    getCurrentTelemetry: vi.fn(),
    getContainerHistory: vi.fn(),
    getTimeSeries: vi.fn(),
    getContainerSettings: vi.fn(),
    setContainerSetpoint: vi.fn(),
    setContainerControl: vi.fn(),
  },
}));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: () => ({ adapter: mocks.adapter, minerRoot: "/miners/ct1", isFleetHosted: true }),
}));

vi.mock("@/shared/features/toaster", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/shared/features/toaster")>()),
  pushToast: mocks.pushToast,
}));

let currentSettings: ContainerSettings;
function configureSettings(settings: ContainerSettings) {
  currentSettings = structuredClone(settings);
  mocks.adapter.getContainerSettings.mockImplementation(async () => structuredClone(currentSettings));
}
const coolerSettings: ContainerSettings = {
  supported: true,
  controls: [
    { name: "dry_cooler", enabled: true },
    { name: "dry_cooler_auto", enabled: true },
  ],
  setpoints: [
    { name: "dry_cooler_auto_temp_c", value: 32, unit: "C" },
    { name: "dry_cooler_manual_speed_percent", value: 80, unit: "%" },
  ],
};

async function selectMode(mode: "Auto" | "Manual") {
  fireEvent.click(await screen.findByRole("button", { name: "Mode" }));
  fireEvent.click(await screen.findByRole("option", { name: mode }));
}

// As in the app router: the System tab shares the root path with Overview.
const HomeIndex = () => (
  <ContainerHomePage tab={useSearchParams()[0].get("view") === "system" ? "system" : "overview"} />
);

const routes = [
  {
    path: "/miners/ct1",
    element: <ContainerHomeLayout />,
    children: [
      { index: true, element: <HomeIndex /> },
      { path: "hashrate", element: <ContainerHomePage tab="hashrate" /> },
      { path: "efficiency", element: <ContainerHomePage tab="efficiency" /> },
      { path: "power-usage", element: <ContainerHomePage tab="power" /> },
      { path: "temperature", element: <ContainerHomePage tab="temperature" /> },
    ],
  },
];

describe("container Home integration", () => {
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  beforeEach(() => {
    vi.resetAllMocks();
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(20, 20, 240, 40));
    useMinerStore.getState().resetDeviceData();
    useMinerStore.getState().ui.setDuration("12h");
    useMinerStore.getState().ui.setFlowUnit("L/min");
    useMinerStore.getState().ui.setTemperatureUnit("C");
    const fixture = toCanonicalContainer(
      {
        containerMap: {
          tanks: [
            { id: 1, modules: [{ id: 1, slots: [{ hashboard: { serial: "HB-1", health: { hashrate: 100 } } }] }] },
          ],
        },
      },
      { hashrate: 900, power: 1500, activeBoards: 3, totalBoards: 3 },
      {
        plant: {
          facilityPowerW: 2000,
          flowRateLpm: 120,
          coolantPumpSpeedHz: 50,
          tankALiquidLevelMm: 860,
          inletTemperatureC: 32,
          outletTemperatureC: 40,
          insideAmbientTemperatureC: 28,
          outsideAmbientTemperatureC: 24,
          pumpOutletPressurePa: 290000,
          dryCoolerInletTemperatureC: 39,
          heatRejectionInletPressurePa: 280000,
          heatRejectionOutletPressurePa: 190000,
        },
        coolingFans: [{ componentId: "fan-one", powerW: 10, speedPercent: 80 }],
        powerDistributions: [{ componentId: "pdu-west", activePowerW: 1500 }],
      },
      "ct1",
    );
    useMinerStore.getState().hardware.replaceHardware(fixture.hardware);
    useMinerStore.getState().telemetry.mergeTelemetry(fixture.telemetry);
    mocks.adapter.getCurrentTelemetry.mockResolvedValue(fixture.telemetry);
    configureSettings({
      supported: true,
      controls: [{ name: "coolant_pump", enabled: true }],
      setpoints: [{ name: "coolant_pump_speed_hz", value: 50, unit: "Hz" }],
    });
    mocks.adapter.setContainerSetpoint.mockImplementation(async (name: string, value: number) => {
      currentSettings.setpoints.find((item) => item.name === name)!.value = value;
    });
    mocks.adapter.setContainerControl.mockImplementation(async (name: string, enabled: boolean) => {
      currentSettings.controls.find((item) => item.name === name)!.enabled = enabled;
    });
    mocks.adapter.getContainerHistory.mockImplementation(async (request: ContainerHistoryRequest) => ({
      series: Object.fromEntries(
        request.selections.map((selection) => [
          containerHistoryKey(selection),
          {
            units: "W",
            bucketSizeMs: request.intervalMs,
            points: [{ timestampMs: Date.parse(request.startTime), value: 10 }],
          },
        ]),
      ),
      unavailable: [],
    }));
  });

  it("shows the schematic above API-backed stats without requesting or polling history", async () => {
    vi.useFakeTimers();
    const router = createMemoryRouter(routes, { initialEntries: ["/miners/ct1/?view=system"] });
    render(<RouterProvider router={router} />);
    await act(async () => {});
    const readings = screen.getByRole("region", { name: "Live readings" });
    const schematic = screen.getByRole("region", { name: "Container cooling system schematic" });
    expect(schematic.compareDocumentPosition(readings) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(screen.queryByRole("figure")).not.toBeInTheDocument();
    expect(screen.queryByText(/History unavailable/)).not.toBeInTheDocument();
    for (const [label, value] of [
      ["Container power", "2.0 kW"],
      ["Flow rate", "120.0 L/min"],
      ["Tank inlet temp", "32.0 °C"],
      ["Tank outlet temp", "40.0 °C"],
      ["Dry cooler outlet pressure", "27.6 psi"],
      ["Pump outlet pressure", "42.1 psi"],
      ["Pump speed", "50.0 Hz"],
      ["Avg fan speed", "80.0 %"],
      ["Internal temp", "28.0 °C"],
      ["Ambient temp", "24.0 °C"],
    ]) {
      expect(
        within(readings).getByRole("heading", { name: label }).closest('[data-testid="stats-item"]'),
      ).toHaveTextContent(value);
    }
    await act(() => vi.advanceTimersByTimeAsync(60_000));
    expect(mocks.adapter.getContainerHistory).not.toHaveBeenCalled();
    expect(mocks.adapter.getCurrentTelemetry.mock.calls.length).toBeGreaterThan(1);
  });

  it("honors Overview unit preferences and distinguishes zero from missing live readings", async () => {
    useMinerStore.getState().ui.setTemperatureUnit("F");
    useMinerStore.getState().ui.setFlowUnit("GPM");
    const router = createMemoryRouter(routes, { initialEntries: ["/miners/ct1/?view=system"] });
    render(<RouterProvider router={router} />);
    const stat = (label: string) =>
      within(screen.getByRole("region", { name: "Live readings" }))
        .getByRole("heading", { name: label })
        .closest('[data-testid="stats-item"]');
    expect(stat("Flow rate")).toHaveTextContent("31.7 GPM");
    expect(stat("Tank inlet temp")).toHaveTextContent("89.6 °F");
    await act(async () => {});
    act(() => {
      const fixture = toCanonicalContainer(
        { containerMap: { tanks: [] } },
        {},
        { plant: { coolantPumpSpeedHz: 0 }, coolingFans: [{ componentId: "fan-one" }] },
        "ct1",
      );
      const store = useMinerStore.getState();
      store.resetDeviceData();
      store.hardware.replaceHardware(fixture.hardware);
      store.telemetry.mergeTelemetry(fixture.telemetry);
    });
    expect(stat("Pump speed")).toHaveTextContent("0.0 Hz");
    expect(stat("Tank outlet temp")).toHaveTextContent("—");
    expect(stat("Avg fan speed")).toHaveTextContent("—");
  });

  it.each(
    [
      ["coolant_pump", "Coolant pump", "Coolant pump power"],
      ["dry_cooler", "Dry cooler", "Dry cooler power"],
      ["air_conditioner_1", "Air conditioner 1", "Air conditioner 1 power"],
      ["air_conditioner_2", "Air conditioner 2", "Air conditioner 2 power"],
      ["tank_led_light", "Tank lights", "Tank lights"],
    ].flatMap(([name, equipment, label]) => [true, false].map((enabled) => ({ name, equipment, label, enabled }))),
  )(
    "confirms $name power changes from $enabled and cancels without writing",
    async ({ name, equipment, label, enabled }) => {
      const names = ["coolant_pump", "dry_cooler", "air_conditioner_1", "air_conditioner_2", "tank_led_light"];
      configureSettings({
        supported: true,
        controls: names.map((name) => ({ name, enabled })),
        setpoints: [],
      });
      const router = createMemoryRouter(routes, { initialEntries: ["/miners/ct1/?view=system"] });
      render(<RouterProvider router={router} />);
      fireEvent.click(screen.getByRole("button", { name: "System controls" }));
      const power = await screen.findByRole("checkbox", { name: label });
      const action = enabled ? "Turn off" : "Turn on";
      fireEvent.click(power);
      expect(mocks.adapter.setContainerControl).not.toHaveBeenCalled();
      expect(power).toHaveProperty("checked", enabled);
      const confirmation = screen.getByRole("dialog", { name: `${action} ${equipment}?` });
      fireEvent.click(within(confirmation).getByRole("button", { name: "Cancel" }));
      expect(screen.queryByRole("dialog", { name: `${action} ${equipment}?` })).not.toBeInTheDocument();
      expect(screen.getByRole("dialog", { name: "System controls" })).toBeInTheDocument();
      expect(mocks.adapter.setContainerControl).not.toHaveBeenCalled();
      fireEvent.click(power);
      fireEvent.keyDown(document, { key: "Escape" });
      expect(screen.queryByRole("dialog", { name: `${action} ${equipment}?` })).not.toBeInTheDocument();
      expect(screen.getByRole("dialog", { name: "System controls" })).toBeInTheDocument();
      expect(mocks.adapter.setContainerControl).not.toHaveBeenCalled();
      fireEvent.click(power);
      fireEvent.click(screen.getByRole("button", { name: action }));
      await waitFor(() =>
        expect(mocks.pushToast).toHaveBeenCalledWith({
          message: `${label} turned ${enabled ? "off" : "on"}.`,
          status: "success",
        }),
      );
      expect(mocks.adapter.setContainerControl).toHaveBeenCalledExactlyOnceWith(name, !enabled, expect.anything());
      expect(
        currentSettings.controls.filter((item) => item.name !== name).every((item) => item.enabled === enabled),
      ).toBe(true);
    },
  );

  it.each([1, 2])("saves air conditioner %s temperature and retains the other target", async (number) => {
    configureSettings({
      supported: true,
      controls: [],
      setpoints: [
        { name: "air_conditioner_1_temp_c", value: 24, unit: "C" },
        { name: "air_conditioner_2_temp_c", value: 24, unit: "C" },
      ],
    });
    const router = createMemoryRouter(routes, { initialEntries: ["/miners/ct1/?view=system"] });
    render(<RouterProvider router={router} />);
    fireEvent.click(screen.getByRole("button", { name: "System controls" }));
    const section = await screen.findByRole("region", { name: `Air conditioner ${number} controls` });
    const input = within(section).getByLabelText("Target temperature (°C)");
    fireEvent.change(input, { target: { value: "26" } });
    fireEvent.blur(input);
    await waitFor(() =>
      expect(mocks.pushToast).toHaveBeenCalledWith({
        message: `Air conditioner ${number} target temperature saved.`,
        status: "success",
      }),
    );
    expect(mocks.adapter.setContainerSetpoint).toHaveBeenCalledExactlyOnceWith(
      `air_conditioner_${number}_temp_c`,
      26,
      expect.anything(),
    );
    expect(
      currentSettings.setpoints.find((item) => item.name === `air_conditioner_${number === 1 ? 2 : 1}_temp_c`)?.value,
    ).toBe(24);
  });

  it("autosaves pump speed on Enter, keeps the editor open, and does not write while typing", async () => {
    const router = createMemoryRouter(routes, { initialEntries: ["/miners/ct1/?view=system"] });
    render(<RouterProvider router={router} />);
    expect(screen.getByRole("img", { name: /Flow rate: 120.0 L\/min/ })).toBeInTheDocument();
    expect(screen.getByText("860.0 mm")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "System controls" }));
    const input = await screen.findByLabelText("Pump speed (Hz)");
    expect(screen.queryByRole("button", { name: "Apply" })).not.toBeInTheDocument();
    fireEvent.change(input, { target: { value: "45" } });
    expect(mocks.adapter.setContainerSetpoint).not.toHaveBeenCalled();
    fireEvent.keyDown(input, { key: "Enter" });
    await waitFor(() =>
      expect(mocks.adapter.setContainerSetpoint).toHaveBeenCalledWith("coolant_pump_speed_hz", 45, expect.anything()),
    );
    await waitFor(() =>
      expect(mocks.pushToast).toHaveBeenCalledWith({ message: "Pump speed saved.", status: "success" }),
    );
    expect(screen.getByRole("dialog", { name: "System controls" })).toBeInTheDocument();
    expect(input).toBeEnabled();
  });

  it("autosaves cooler mode on selection and fan percentage on blur", async () => {
    configureSettings(coolerSettings);
    const router = createMemoryRouter(routes, { initialEntries: ["/miners/ct1/?view=system"] });
    render(<RouterProvider router={router} />);
    fireEvent.click(screen.getByRole("button", { name: "System controls" }));
    await selectMode("Manual");
    const input = await screen.findByLabelText("Fan speed (%)");
    await waitFor(() => expect(input).toBeEnabled());
    expect(mocks.adapter.setContainerControl).toHaveBeenCalledWith("dry_cooler_auto", false, expect.anything());
    expect(mocks.adapter.setContainerSetpoint).not.toHaveBeenCalled();
    fireEvent.change(input, { target: { value: "65" } });
    fireEvent.blur(input);
    await waitFor(() =>
      expect(mocks.pushToast).toHaveBeenCalledWith({ message: "Fan speed saved.", status: "success" }),
    );
    expect(mocks.adapter.setContainerSetpoint).toHaveBeenCalledWith(
      "dry_cooler_manual_speed_percent",
      65,
      expect.anything(),
    );
    expect(screen.getByRole("dialog", { name: "System controls" })).toBeInTheDocument();
  });

  it("confirms power and waits for readback before showing success", async () => {
    let completeRead!: (settings: ContainerSettings) => void;
    const router = createMemoryRouter(routes, { initialEntries: ["/miners/ct1/?view=system"] });
    render(<RouterProvider router={router} />);
    fireEvent.click(screen.getByRole("button", { name: "System controls" }));
    const power = await screen.findByRole("checkbox", { name: "Coolant pump power" });
    mocks.adapter.getContainerSettings.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          completeRead = resolve;
        }),
    );
    fireEvent.click(power);
    expect(mocks.adapter.setContainerControl).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Turn off" }));
    await waitFor(() =>
      expect(mocks.adapter.setContainerControl).toHaveBeenCalledWith("coolant_pump", false, expect.anything()),
    );
    expect(power).toBeDisabled();
    expect(power).toBeChecked();
    expect(mocks.pushToast).not.toHaveBeenCalled();
    await waitFor(() => expect(completeRead).toBeDefined());
    await act(async () => completeRead(structuredClone(currentSettings)));
    await waitFor(() => expect(power).toBeEnabled());
    expect(power).not.toBeChecked();
    expect(mocks.pushToast).toHaveBeenCalledWith({ message: "Coolant pump power turned off.", status: "success" });
  });

  it.each(["write", "readback", "telemetry"] as const)(
    "recovers from a stalled %s without replaying the write",
    async (stage) => {
      const router = createMemoryRouter(routes, { initialEntries: ["/miners/ct1/?view=system"] });
      render(<RouterProvider router={router} />);
      fireEvent.click(screen.getByRole("button", { name: "System controls" }));
      const input = await screen.findByLabelText("Pump speed (Hz)");
      await waitFor(() => expect(mocks.adapter.getCurrentTelemetry).toHaveBeenCalledTimes(1));
      vi.useFakeTimers();
      if (stage === "write") {
        mocks.adapter.setContainerSetpoint.mockImplementationOnce((name: string, value: number) => {
          currentSettings.setpoints.find((item) => item.name === name)!.value = value;
          return new Promise(() => {});
        });
      } else if (stage === "readback") {
        mocks.adapter.getContainerSettings.mockImplementationOnce(() => new Promise(() => {}));
      } else {
        mocks.adapter.getCurrentTelemetry.mockImplementationOnce(() => new Promise(() => {}));
      }
      fireEvent.change(input, { target: { value: "45" } });
      fireEvent.keyDown(input, { key: "Enter" });
      await act(async () => {});
      expect(input).toBeDisabled();
      const signal =
        stage === "write"
          ? mocks.adapter.setContainerSetpoint.mock.calls[0][2].signal
          : stage === "readback"
            ? mocks.adapter.getContainerSettings.mock.calls[1][0].signal
            : mocks.adapter.getCurrentTelemetry.mock.calls[1][1]?.signal;
      await act(() => vi.advanceTimersByTimeAsync(stage === "write" ? 30_000 : 15_000));
      expect(input).toBeEnabled();
      expect(signal?.aborted).toBe(true);
      expect(input).toHaveValue("45");
      expect(mocks.pushToast).toHaveBeenCalledWith(expect.objectContaining({ status: "error" }));
      if (stage !== "telemetry") {
        expect(input).toHaveAttribute("aria-invalid", "true");
        fireEvent.keyDown(input, { key: "Enter" });
        await act(async () => {});
        expect(mocks.pushToast).toHaveBeenCalledWith({ message: "Pump speed saved.", status: "success" });
      }
      expect(mocks.adapter.setContainerSetpoint).toHaveBeenCalledTimes(1);
      fireEvent.click(screen.getByRole("button", { name: "Close dialog" }));
      await act(async () => {});
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    },
  );

  it.each(["Mode", "Power"] as const)(
    "preserves a native %s click while numeric blur waits for readback",
    async (control) => {
      const user = userEvent.setup();
      configureSettings(coolerSettings);
      const router = createMemoryRouter(routes, { initialEntries: ["/miners/ct1/?view=system"] });
      render(<RouterProvider router={router} />);
      fireEvent.click(screen.getByRole("button", { name: "System controls" }));
      const input = await screen.findByLabelText("Target inlet temperature (°C)");
      await user.clear(input);
      await user.type(input, "35");
      let completeRead!: (settings: ContainerSettings) => void;
      mocks.adapter.getContainerSettings.mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            completeRead = resolve;
          }),
      );
      try {
        if (control === "Mode") {
          await user.click(screen.getByRole("button", { name: "Mode" }));
          expect(screen.getByRole("option", { name: "Manual" })).toBeVisible();
          await user.click(screen.getByRole("option", { name: "Manual" }));
        } else {
          await user.click(screen.getByRole("checkbox", { name: "Dry cooler power" }));
          await user.click(screen.getByRole("button", { name: "Turn off" }));
        }
        expect(mocks.adapter.setContainerSetpoint).toHaveBeenCalledTimes(1);
        expect(mocks.adapter.setContainerControl).not.toHaveBeenCalled();
      } finally {
        await act(async () => completeRead?.(structuredClone(currentSettings)));
      }
      await waitFor(() =>
        expect(mocks.adapter.setContainerControl).toHaveBeenCalledWith(
          control === "Mode" ? "dry_cooler_auto" : "dry_cooler",
          false,
          expect.anything(),
        ),
      );
      await waitFor(() =>
        expect(mocks.pushToast).toHaveBeenCalledWith({
          message: control === "Mode" ? "Mode set to Manual." : "Dry cooler power turned off.",
          status: "success",
        }),
      );
      expect(mocks.adapter.setContainerSetpoint).toHaveBeenCalledTimes(1);
      expect(mocks.adapter.setContainerControl).toHaveBeenCalledTimes(1);
    },
  );

  it("discards a power confirmation when the device connection changes", async () => {
    const router = createMemoryRouter(routes, { initialEntries: ["/miners/ct1/?view=system"] });
    render(<RouterProvider router={router} />);
    fireEvent.click(screen.getByRole("button", { name: "System controls" }));
    fireEvent.click(await screen.findByRole("checkbox", { name: "Coolant pump power" }));
    expect(screen.getByRole("dialog", { name: "Turn off Coolant pump?" })).toBeInTheDocument();
    act(() => useMinerStore.getState().resetDeviceData());
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(mocks.adapter.setContainerControl).not.toHaveBeenCalled();
  });

  it("keeps an unknown power state unavailable", async () => {
    configureSettings({ supported: true, controls: [{ name: "coolant_pump" }], setpoints: [] });
    const router = createMemoryRouter(routes, { initialEntries: ["/miners/ct1/?view=system"] });
    render(<RouterProvider router={router} />);
    fireEvent.click(screen.getByRole("button", { name: "System controls" }));
    expect(await screen.findByRole("checkbox", { name: "Coolant pump power" })).toBeDisabled();
    expect(screen.getByText("Unavailable")).toBeInTheDocument();
    expect(mocks.adapter.setContainerControl).not.toHaveBeenCalled();
  });

  it("refreshes untouched setpoints from the device without overwriting them on a mode change", async () => {
    configureSettings(coolerSettings);
    const router = createMemoryRouter(routes, { initialEntries: ["/miners/ct1/?view=system"] });
    render(<RouterProvider router={router} />);
    fireEvent.click(screen.getByRole("button", { name: "System controls" }));
    const target = await screen.findByLabelText("Target inlet temperature (°C)");
    currentSettings.setpoints.find((item) => item.name === "dry_cooler_auto_temp_c")!.value = 36;
    fireEvent.click(screen.getByRole("checkbox", { name: "Dry cooler power" }));
    fireEvent.click(screen.getByRole("button", { name: "Turn off" }));
    await waitFor(() => expect(target).toHaveValue("36"));
    await waitFor(() => expect(target).toBeEnabled());
    await selectMode("Manual");
    await waitFor(() =>
      expect(mocks.pushToast).toHaveBeenCalledWith({ message: "Mode set to Manual.", status: "success" }),
    );
    expect(mocks.adapter.setContainerSetpoint).not.toHaveBeenCalled();
  });

  it("validates numeric edits and retains rejected drafts with an error toast", async () => {
    mocks.adapter.setContainerSetpoint.mockRejectedValueOnce(new Error("PLC rejected command"));
    const router = createMemoryRouter(routes, { initialEntries: ["/miners/ct1/?view=system"] });
    render(<RouterProvider router={router} />);
    fireEvent.click(screen.getByRole("button", { name: "System controls" }));
    const input = await screen.findByLabelText("Pump speed (Hz)");
    fireEvent.change(input, { target: { value: "1e99" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(mocks.adapter.setContainerSetpoint).not.toHaveBeenCalled();
    fireEvent.change(input, { target: { value: "45" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(await screen.findByText("PLC rejected command")).toBeInTheDocument();
    expect(mocks.pushToast).toHaveBeenCalledWith({
      message: "Could not save pump speed: PLC rejected command",
      status: "error",
    });
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(input).toHaveValue("45");
    expect(currentSettings.setpoints[0].value).toBe(50);
  });

  it("converts Fahrenheit and saves a dirty target before changing mode without replaying it on retry", async () => {
    useMinerStore.getState().ui.setTemperatureUnit("F");
    configureSettings(coolerSettings);
    mocks.adapter.setContainerControl.mockRejectedValueOnce(new Error("Mode command failed"));
    const router = createMemoryRouter(routes, { initialEntries: ["/miners/ct1/?view=system"] });
    render(<RouterProvider router={router} />);
    fireEvent.click(screen.getByRole("button", { name: "System controls" }));
    const input = await screen.findByLabelText("Target inlet temperature (°F)");
    fireEvent.change(input, { target: { value: "104" } });
    await selectMode("Manual");
    expect(await screen.findByText("Mode command failed")).toBeInTheDocument();
    expect(mocks.adapter.setContainerSetpoint).toHaveBeenCalledWith("dry_cooler_auto_temp_c", 40, expect.anything());
    expect(mocks.adapter.setContainerSetpoint.mock.invocationCallOrder[0]).toBeLessThan(
      mocks.adapter.setContainerControl.mock.invocationCallOrder[0],
    );
    await waitFor(() => expect(screen.getByRole("button", { name: "Mode" })).toBeEnabled());
    await selectMode("Manual");
    await waitFor(() =>
      expect(mocks.pushToast).toHaveBeenCalledWith({ message: "Mode set to Manual.", status: "success" }),
    );
    expect(mocks.adapter.setContainerSetpoint).toHaveBeenCalledTimes(1);
    expect(mocks.adapter.setContainerControl).toHaveBeenCalledTimes(2);
  });

  it("does not change mode after an explicit setpoint readback failure", async () => {
    const write = vi.fn().mockResolvedValue({
      data: { confirmation: { state: "PLANT_WRITE_CONFIRMATION_STATE_MISMATCH", observedValue: 32 } },
    });
    const adapter = new MdkV2Adapter({ powerServiceSetContainerSetpoint: write } as unknown as MdkV2Client);
    mocks.adapter.setContainerSetpoint.mockImplementation(adapter.setContainerSetpoint.bind(adapter));
    configureSettings(coolerSettings);
    const router = createMemoryRouter(routes, { initialEntries: ["/miners/ct1/?view=system"] });
    render(<RouterProvider router={router} />);
    fireEvent.click(screen.getByRole("button", { name: "System controls" }));
    const input = await screen.findByLabelText("Target inlet temperature (°C)");
    fireEvent.change(input, { target: { value: "35" } });
    await selectMode("Manual");
    expect(await screen.findByText("Save the current setting before changing mode.")).toBeInTheDocument();
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(mocks.adapter.setContainerControl).not.toHaveBeenCalled();
    expect(mocks.pushToast.mock.calls.every(([toast]) => toast.status === "error")).toBe(true);
  });

  it("deduplicates Enter and blur, and retries failed readback without resending the saved value", async () => {
    const router = createMemoryRouter(routes, { initialEntries: ["/miners/ct1/?view=system"] });
    render(<RouterProvider router={router} />);
    fireEvent.click(screen.getByRole("button", { name: "System controls" }));
    const input = await screen.findByLabelText("Pump speed (Hz)");
    mocks.adapter.getContainerSettings.mockRejectedValueOnce(new Error("Connection interrupted"));
    fireEvent.change(input, { target: { value: "45" } });
    fireEvent.keyDown(input, { key: "Enter" });
    fireEvent.blur(input);
    expect(await screen.findByText("Could not refresh settings: Connection interrupted")).toBeInTheDocument();
    await waitFor(() => expect(input).toBeEnabled());
    expect(mocks.pushToast).not.toHaveBeenCalledWith(expect.objectContaining({ status: "success" }));
    fireEvent.keyDown(input, { key: "Enter" });
    await waitFor(() =>
      expect(mocks.pushToast).toHaveBeenCalledWith({ message: "Pump speed saved.", status: "success" }),
    );
    expect(mocks.adapter.setContainerSetpoint).toHaveBeenCalledTimes(1);
  });

  it("stops queued writes and notifications when the device connection changes", async () => {
    let completeWrite!: () => void;
    mocks.adapter.setContainerSetpoint.mockImplementationOnce(
      () =>
        new Promise<void>((resolve) => {
          completeWrite = resolve;
        }),
    );
    configureSettings(coolerSettings);
    const router = createMemoryRouter(routes, { initialEntries: ["/miners/ct1/?view=system"] });
    render(<RouterProvider router={router} />);
    fireEvent.click(screen.getByRole("button", { name: "System controls" }));
    const input = await screen.findByLabelText("Target inlet temperature (°C)");
    fireEvent.change(input, { target: { value: "35" } });
    await selectMode("Manual");
    await waitFor(() => expect(mocks.adapter.setContainerSetpoint).toHaveBeenCalledTimes(1));
    act(() => useMinerStore.getState().resetDeviceData());
    await act(async () => completeWrite());
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(mocks.adapter.setContainerControl).not.toHaveBeenCalled();
    expect(mocks.pushToast).not.toHaveBeenCalled();
  });

  it("keeps a manual cooler draft visible after a failed refresh and recovers without replaying it", async () => {
    configureSettings(coolerSettings);
    const router = createMemoryRouter(routes, { initialEntries: ["/miners/ct1/?view=system"] });
    render(<RouterProvider router={router} />);
    fireEvent.click(screen.getByRole("button", { name: "System controls" }));
    await selectMode("Manual");
    const input = await screen.findByLabelText("Fan speed (%)");
    await waitFor(() => expect(input).toBeEnabled());
    mocks.adapter.getContainerSettings.mockRejectedValueOnce(new Error("Connection interrupted"));
    fireEvent.change(input, { target: { value: "65" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(await screen.findByText("Could not refresh settings: Connection interrupted")).toBeInTheDocument();
    expect(input).toHaveValue("65");
    await waitFor(() => expect(input).toBeEnabled());
    fireEvent.keyDown(input, { key: "Enter" });
    await waitFor(() =>
      expect(mocks.pushToast).toHaveBeenCalledWith({ message: "Fan speed saved.", status: "success" }),
    );
    expect(mocks.adapter.setContainerSetpoint).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("button", { name: "Mode" })).toHaveTextContent("Manual");
  });

  it("keeps one live polling owner across duration and tab changes, with no per-board histories", async () => {
    const router = createMemoryRouter(routes, { initialEntries: ["/miners/ct1/?view=system"] });
    render(<RouterProvider router={router} />);
    await waitFor(() => expect(mocks.adapter.getCurrentTelemetry).toHaveBeenCalledTimes(1));
    expect(mocks.adapter.getContainerHistory).not.toHaveBeenCalled();
    expect(mocks.adapter.getCurrentTelemetry).toHaveBeenCalledTimes(1);
    expect(screen.getAllByRole("navigation", { name: "Container Home" })).toHaveLength(1);
    expect(screen.queryByRole("button", { name: "48h" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("link", { name: "Hashrate" }));
    await waitFor(() => expect(mocks.adapter.getContainerHistory).toHaveBeenCalledTimes(1));

    fireEvent.click(screen.getByRole("button", { name: "48h" }));
    await waitFor(() => expect(mocks.adapter.getContainerHistory).toHaveBeenCalledTimes(2));
    expect(mocks.adapter.getCurrentTelemetry).toHaveBeenCalledTimes(1);
    expect(screen.getByText("100.0 TH/s")).toBeInTheDocument();
    const request = mocks.adapter.getContainerHistory.mock.calls[1][0];
    expect(Date.parse(request.endTime) - Date.parse(request.startTime)).toBe(48 * 3_600_000);

    fireEvent.click(screen.getByRole("link", { name: "Power" }));
    await waitFor(() => expect(mocks.adapter.getContainerHistory).toHaveBeenCalledTimes(3));
    expect(mocks.adapter.getCurrentTelemetry).toHaveBeenCalledTimes(1);
    expect(mocks.adapter.getContainerHistory.mock.calls[2][0].selections).toEqual([
      { metric: "facilityPower" },
      { metric: "power" },
      { metric: "distributionPower", identifier: "pdu-west" },
      { metric: "fanPower", identifier: "fan-one" },
    ]);
    expect(screen.queryByRole("button", { name: "Hashboards" })).not.toBeInTheDocument();
    expect(mocks.adapter.getTimeSeries).not.toHaveBeenCalled();
    expect(mocks.adapter.getCurrentTelemetry).toHaveBeenCalledTimes(1);
  });

  it("refreshes live module summaries with one bulk map call per poll and no board histories", async () => {
    vi.useFakeTimers();
    const map = {
      containerMap: {
        tanks: [
          {
            id: 1,
            modules: [
              {
                id: 1,
                slots: [
                  {
                    hashboard: {
                      serial: "HB-1",
                      health: { temperatures: [20], psuElectrical: { inputPower: 4000 } },
                    },
                  },
                ],
              },
            ],
          },
        ],
      },
    };
    const fixture = toCanonicalContainer(map, {}, {}, "ct1");
    useMinerStore.getState().hardware.replaceHardware(fixture.hardware);
    const client = {
      hashboardServiceGetContainerMap: vi.fn().mockResolvedValue({ data: map }),
      miningServiceGetMiningStatus: vi.fn().mockResolvedValue({ data: {} }),
      telemetryServiceGetContainerHealth: vi.fn().mockResolvedValue({ data: {} }),
      telemetryServiceQueryTimeSeries: vi.fn(),
    };
    const adapter = new MdkV2Adapter(client as unknown as MdkV2Client, { deviceId: "ct1" });
    mocks.adapter.getCurrentTelemetry.mockImplementation(adapter.getCurrentTelemetry.bind(adapter));
    const router = createMemoryRouter(routes, { initialEntries: ["/miners/ct1/power-usage"] });
    render(<RouterProvider router={router} />);
    await act(async () => {});
    expect(screen.getAllByText("4.0 kW")).toHaveLength(3);
    expect(client.hashboardServiceGetContainerMap).toHaveBeenCalledTimes(1);

    map.containerMap.tanks[0].modules[0].slots[0].hashboard.health.psuElectrical.inputPower = 6000;
    await act(() => vi.advanceTimersByTimeAsync(15_000));
    expect(screen.getAllByText("6.0 kW")).toHaveLength(3);
    expect(client.hashboardServiceGetContainerMap).toHaveBeenCalledTimes(2);
    expect(client.miningServiceGetMiningStatus).toHaveBeenCalledTimes(2);
    expect(client.telemetryServiceGetContainerHealth).toHaveBeenCalledTimes(2);

    await act(async () => fireEvent.click(screen.getByRole("link", { name: "Temperature" })));
    expect(screen.getAllByText("20.0 °C")).toHaveLength(3);
    expect(client.hashboardServiceGetContainerMap).toHaveBeenCalledTimes(2);
    expect(client.telemetryServiceQueryTimeSeries).not.toHaveBeenCalled();
    expect(mocks.adapter.getTimeSeries).not.toHaveBeenCalled();
  });

  it("preserves board-health hashrate after saving equipment and navigating from System", async () => {
    vi.useFakeTimers();
    const boards = [
      { serial: "HB-1", health: { hashrate: 60 } },
      { serial: "HB-2", health: { hashrate: 40 } },
    ];
    const map = {
      containerMap: {
        tanks: [{ id: 1, modules: [{ id: 1, slots: boards.map((hashboard) => ({ hashboard })) }] }],
      },
    };
    const fixture = toCanonicalContainer(map, {}, {}, "ct1");
    useMinerStore.getState().hardware.replaceHardware(fixture.hardware);
    const client = {
      hashboardServiceGetContainerMap: vi.fn().mockResolvedValue({ data: map }),
      miningServiceGetMiningStatus: vi.fn().mockResolvedValue({ data: { hashrate: 900 } }),
      telemetryServiceGetContainerHealth: vi.fn().mockResolvedValue({ data: {} }),
    };
    const adapter = new MdkV2Adapter(client as unknown as MdkV2Client, { deviceId: "ct1" });
    mocks.adapter.getCurrentTelemetry.mockImplementation(adapter.getCurrentTelemetry.bind(adapter));
    const router = createMemoryRouter(routes, { initialEntries: ["/miners/ct1/?view=system"] });
    render(<RouterProvider router={router} />);
    await act(async () => {});
    expect(mocks.adapter.getContainerHistory).not.toHaveBeenCalled();
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "System controls" })));
    const speed = screen.getByLabelText("Pump speed (Hz)");
    fireEvent.change(speed, { target: { value: "45" } });
    await act(async () => fireEvent.keyDown(speed, { key: "Enter" }));
    expect(mocks.pushToast).toHaveBeenCalledWith({ message: "Pump speed saved.", status: "success" });
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Close dialog" })));
    await act(async () => fireEvent.click(screen.getByRole("link", { name: "Hashrate" })));
    expect(screen.getByText("100.0 TH/s")).toBeInTheDocument();
    expect(screen.queryByText("900.0 TH/s")).not.toBeInTheDocument();

    boards[0].health.hashrate = 80;
    await act(() => vi.advanceTimersByTimeAsync(15_000));
    expect(screen.getByText("120.0 TH/s")).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "Live readings" })).toHaveTextContent("Hashrate120.0 TH/s");
    expect(screen.queryByText(/Accepted-share/)).not.toBeInTheDocument();
    expect(client.hashboardServiceGetContainerMap).toHaveBeenCalledTimes(3);
    expect(client.miningServiceGetMiningStatus).toHaveBeenCalledTimes(3);
  });

  it("summarizes the container on Overview from one combined history request", async () => {
    const router = createMemoryRouter(routes, { initialEntries: ["/miners/ct1/"] });
    render(<RouterProvider router={router} />);
    await waitFor(() => expect(mocks.adapter.getContainerHistory).toHaveBeenCalledTimes(1));
    expect(mocks.adapter.getContainerHistory.mock.calls[0][0].selections).toEqual([
      { metric: "hashrate" },
      { metric: "facilityPower" },
      { metric: "outletATemperature" },
      { metric: "outletBTemperature" },
    ]);
    expect(screen.getByRole("link", { name: "Overview" })).toHaveAttribute("aria-current", "page");
    expect(screen.getByRole("link", { name: "View Efficiency" })).toHaveAttribute("href", "/miners/ct1/efficiency");
    expect(screen.getByRole("heading", { name: "Active issues" })).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Container cooling system schematic" })).not.toBeInTheDocument();
    expect(mocks.adapter.getTimeSeries).not.toHaveBeenCalled();
  });

  it("shows and polls supported power histories even with an obsolete Hashboards query", async () => {
    vi.useFakeTimers();
    mocks.adapter.getContainerHistory.mockImplementation(async (request: ContainerHistoryRequest) => ({
      series: Object.fromEntries(
        request.selections.map((selection) => [
          containerHistoryKey(selection),
          {
            units: "W",
            bucketSizeMs: request.intervalMs,
            points: [
              {
                timestampMs: Date.parse(request.endTime) - request.intervalMs,
                value:
                  selection.metric === "facilityPower" ? 2000 : selection.metric === "distributionPower" ? 0 : 1000,
              },
            ],
          },
        ]),
      ),
      unavailable: [],
    }));
    const router = createMemoryRouter(routes, { initialEntries: ["/miners/ct1/power-usage?mode=hashboards"] });
    render(<RouterProvider router={router} />);
    await act(async () => {});
    expect(screen.queryByRole("group", { name: "Power chart mode" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Hashboards" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "System" })).not.toBeInTheDocument();
    for (const label of ["Pumps", "AC", "Aux"]) expect(screen.queryByText(label)).not.toBeInTheDocument();
    expect(screen.getByRole("region", { name: "Fans period statistics" })).toBeInTheDocument();
    const legend = screen.getByRole("list", { name: "Latest series values" });
    expect(legend).toHaveTextContent("Facility total2.0 kW");
    expect(legend).toHaveTextContent("Mining power1.0 kW");
    expect(legend).toHaveTextContent("pdu-west0.0 kW");
    expect(screen.getByRole("region", { name: "Live readings" })).not.toHaveTextContent("pdu-west");
    expect(legend).toHaveTextContent("Fans1.0 kW");
    expect(mocks.adapter.getContainerHistory.mock.calls[0][0].selections).toEqual([
      { metric: "facilityPower" },
      { metric: "power" },
      { metric: "distributionPower", identifier: "pdu-west" },
      { metric: "fanPower", identifier: "fan-one" },
    ]);
    await act(() => vi.advanceTimersByTimeAsync(30_000));
    expect(mocks.adapter.getContainerHistory).toHaveBeenCalledTimes(2);
    expect(mocks.adapter.getTimeSeries).not.toHaveBeenCalled();
  });

  it("keeps successful power series when a PDU history is unavailable", async () => {
    mocks.adapter.getContainerHistory.mockImplementation(async (request: ContainerHistoryRequest) => ({
      series: {
        "facilityPower:": {
          units: "W",
          bucketSizeMs: request.intervalMs,
          points: [{ timestampMs: Date.parse(request.endTime) - request.intervalMs, value: 2000 }],
        },
      },
      unavailable: ["distributionPower:pdu-west", "power:", "fanPower:fan-one"],
    }));
    render(<RouterProvider router={createMemoryRouter(routes, { initialEntries: ["/miners/ct1/power-usage"] })} />);
    const legend = await screen.findByRole("list", { name: "Latest series values" });
    await waitFor(() => expect(legend).toHaveTextContent("Facility total2.0 kW"));
    expect(legend).toHaveTextContent("pdu-west—");
    expect(screen.getByRole("region", { name: "Fans period statistics" })).toHaveTextContent("—");
  });

  it("supports direct Fluid entry and back/forward navigation without unrelated history queries", async () => {
    const router = createMemoryRouter(routes, { initialEntries: ["/miners/ct1/temperature?mode=fluid"] });
    render(<RouterProvider router={router} />);
    await act(async () => {});
    expect(screen.getByRole("button", { name: "Fluid" })).toHaveAttribute("aria-pressed", "true");
    expect(mocks.adapter.getContainerHistory).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "System" }));
    await waitFor(() => expect(mocks.adapter.getContainerHistory).toHaveBeenCalledTimes(1));
    await act(() => router.navigate(-1));
    expect(screen.getByRole("button", { name: "Fluid" })).toHaveAttribute("aria-pressed", "true");
  });

  it("prefetches the container page standalone and when Fleet-hosted", () => {
    expect(containerRoutePrefetch).toContain(importContainerHome);
    expect(singleMinerRoutePrefetch).toContain(importContainerHome);
  });

  it("keeps efficiency history on facility power when the live facility reading is missing", async () => {
    const fixture = toCanonicalContainer(
      { containerMap: { tanks: [] } },
      { hashrate: 100, power: 1500 },
      { plant: { totalPowerW: 1500 } },
      "ct1",
    );
    useMinerStore.getState().resetDeviceData();
    useMinerStore.getState().hardware.replaceHardware(fixture.hardware);
    useMinerStore.getState().telemetry.mergeTelemetry(fixture.telemetry);
    mocks.adapter.getCurrentTelemetry.mockResolvedValue(fixture.telemetry);
    mocks.adapter.getContainerHistory.mockResolvedValue({ series: {}, unavailable: ["facilityPower:"] });
    const router = createMemoryRouter(routes, { initialEntries: ["/miners/ct1/efficiency"] });
    render(<RouterProvider router={router} />);
    await waitFor(() => expect(mocks.adapter.getContainerHistory).toHaveBeenCalledTimes(1));
    expect(mocks.adapter.getContainerHistory.mock.calls[0][0].selections).toEqual([
      { metric: "hashrate" },
      { metric: "facilityPower" },
    ]);
    expect(screen.queryByText("15.0 J/TH")).not.toBeInTheDocument();
    expect(screen.getByText("History unavailable for this selection.")).toBeVisible();
    expect(mocks.adapter.getTimeSeries).not.toHaveBeenCalled();
  });
});
