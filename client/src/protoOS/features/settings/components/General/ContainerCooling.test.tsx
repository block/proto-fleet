import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";
import ContainerCooling from "./ContainerCooling";
import {
  resetFanReversalOutcome,
  setFanReversalOutcome,
} from "@/protoOS/features/containerHome/hooks/useFanReversalOutcome";
import useMinerStore from "@/protoOS/store/useMinerStore";

const adapter = vi.hoisted(() => ({
  getContainerSettings: vi.fn(),
  setContainerControl: vi.fn(),
  setContainerSetpoint: vi.fn(),
}));
const preferences = vi.hoisted(() => ({ temperatureUnit: "C" }));
vi.mock("@/protoOS/store", () => ({ useTemperatureUnit: () => preferences.temperatureUnit }));
// The dry cooler hold reads through useAuthRetry; its token refresh is not exercised here.
vi.mock("@/protoOS/store/hooks/useAuth", () => ({ useAuthErrors: () => ({ handleAuthErrors: vi.fn() }) }));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({ useMinerHosting: () => ({ adapter }) }));

beforeEach(() => {
  vi.resetAllMocks();
  resetFanReversalOutcome();
  preferences.temperatureUnit = "C";
  adapter.getContainerSettings.mockResolvedValue({
    supported: true,
    controls: [{ name: "coolant_pump" }],
    setpoints: [{ name: "dry_cooler_auto_temp_c", value: 38, unit: "°C" }],
  });
});

test("unknown controls require an explicit state and refresh after acknowledgement", async () => {
  render(<ContainerCooling />);
  fireEvent.click(await screen.findByRole("button", { name: "Edit Coolant pump" }));
  expect(screen.getByRole("button", { name: "Apply" })).toBeDisabled();
  fireEvent.click(screen.getByRole("button", { name: "On", pressed: false }));
  fireEvent.click(screen.getByRole("button", { name: "Apply" }));
  await waitFor(() => expect(adapter.setContainerControl).toHaveBeenCalledWith("coolant_pump", true));
  await waitFor(() => expect(adapter.getContainerSettings).toHaveBeenCalledTimes(2));
});

test("setpoints reject empty and non-finite input and save in reported units", async () => {
  render(<ContainerCooling />);
  fireEvent.click(await screen.findByRole("button", { name: "Edit Dry cooler target inlet temperature" }));
  const input = screen.getByLabelText("Dry cooler target inlet temperature (°C)");
  for (const value of ["", "NaN", "1e99"]) {
    fireEvent.change(input, { target: { value } });
    expect(screen.getByRole("button", { name: "Apply" })).toBeDisabled();
  }
  fireEvent.change(input, { target: { value: "39.5" } });
  fireEvent.click(screen.getByRole("button", { name: "Apply" }));
  await waitFor(() => expect(adapter.setContainerSetpoint).toHaveBeenCalledWith("dry_cooler_auto_temp_c", 39.5));
});

test("a rejected command shows its error in the editor and refreshes automatically", async () => {
  adapter.setContainerControl.mockRejectedValue(new Error("PLC rejected command"));
  render(<ContainerCooling />);
  fireEvent.click(await screen.findByRole("button", { name: "Edit Coolant pump" }));
  fireEvent.click(screen.getByRole("button", { name: "Off", pressed: false }));
  fireEvent.click(screen.getByRole("button", { name: "Apply" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Could not apply this change.");
  await waitFor(() => expect(adapter.getContainerSettings).toHaveBeenCalledTimes(2));
  expect(screen.queryByText("Reload controls")).not.toBeInTheDocument();
});

test("a backend without plant controls does not display edit actions", async () => {
  adapter.getContainerSettings.mockResolvedValue({ supported: false, controls: [], setpoints: [] });
  render(<ContainerCooling />);
  await waitFor(() => expect(adapter.getContainerSettings).toHaveBeenCalled());
  expect(screen.getByRole("heading", { name: "Coolant pump" }).parentElement).toHaveTextContent("—");
  expect(screen.queryByText("Controls are not available on this container.")).not.toBeInTheDocument();
  expect(screen.queryByText("Edit")).not.toBeInTheDocument();
});

test("loading and failed reads show dashes without error or reload messages", async () => {
  let rejectRead!: (error: Error) => void;
  adapter.getContainerSettings.mockReturnValue(
    new Promise((_, reject) => {
      rejectRead = reject;
    }),
  );
  render(<ContainerCooling />);
  expect(screen.getAllByText("—")).toHaveLength(11);
  expect(screen.queryByText(/Loading controls/)).not.toBeInTheDocument();
  rejectRead(new Error("offline"));
  await waitFor(() => expect(adapter.getContainerSettings).toHaveBeenCalledTimes(1));
  expect(screen.getAllByText("—")).toHaveLength(11);
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(screen.queryByText("Reload controls")).not.toBeInTheDocument();
  expect(screen.queryByText("Edit")).not.toBeInTheDocument();
});

test("Fahrenheit inputs are converted to Celsius for the controller", async () => {
  preferences.temperatureUnit = "F";
  render(<ContainerCooling />);
  fireEvent.click(await screen.findByRole("button", { name: "Edit Dry cooler target inlet temperature" }));
  fireEvent.change(screen.getByLabelText("Dry cooler target inlet temperature (°F)"), { target: { value: "104" } });
  fireEvent.click(screen.getByRole("button", { name: "Apply" }));
  await waitFor(() => expect(adapter.setContainerSetpoint).toHaveBeenCalledWith("dry_cooler_auto_temp_c", 40));
});

test("dry cooler settings cannot be changed while a fan reversal runs", async () => {
  adapter.getContainerSettings.mockResolvedValue({
    supported: true,
    controls: [
      { name: "coolant_pump", enabled: true },
      { name: "dry_cooler", enabled: true },
    ],
    setpoints: [{ name: "dry_cooler_auto_temp_c", value: 38, unit: "°C" }],
    dryCoolerFanReversal: { status: { state: "stopped" } },
  });
  render(<ContainerCooling />);
  fireEvent.click(await screen.findByRole("button", { name: "Edit Dry cooler" }));
  fireEvent.click(screen.getByRole("button", { name: "Off", pressed: false }));

  // The run starts, here or in System controls, after the editor opened.
  act(() =>
    setFanReversalOutcome(useMinerStore.getState().connectionGeneration, { phase: "running", durationMinutes: 5 }),
  );
  expect(screen.getByRole("button", { name: "Apply" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Edit Dry cooler target inlet temperature" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Edit Coolant pump" })).toBeEnabled();
  expect(screen.getByText("Dry cooler settings are locked while the fan reversal runs.")).toBeInTheDocument();
  expect(adapter.setContainerControl).not.toHaveBeenCalled();
});

test("a fan reversal the controller reports locks the dry cooler settings", async () => {
  adapter.getContainerSettings.mockResolvedValue({
    supported: true,
    controls: [{ name: "dry_cooler", enabled: true }],
    setpoints: [],
    dryCoolerFanReversal: { status: { state: "inProgress", durationMinutes: 5 } },
  });
  render(<ContainerCooling />);
  await waitFor(() => expect(screen.getByRole("button", { name: "Edit Dry cooler" })).toBeDisabled());
});
