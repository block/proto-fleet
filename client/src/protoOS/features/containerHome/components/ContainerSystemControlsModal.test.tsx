import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import ContainerSystemControlsModal from "./ContainerSystemControlsModal";
import { BREAKER_OUTCOME_UNKNOWN } from "@/protoOS/api/adapters/breakerMessages";
import useMinerStore from "@/protoOS/store/useMinerStore";

const mocks = vi.hoisted(() => ({
  changeConnectionAfterSuccess: false,
  isFleetHosted: false,
  adapter: {
    getContainerSettings: vi.fn(),
    setContainerControl: vi.fn(),
    setContainerSetpoint: vi.fn(),
    setMainCircuitBreakerState: vi.fn(),
    getCurrentTelemetry: vi.fn(),
  },
}));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: () => ({ adapter: mocks.adapter, isFleetHosted: mocks.isFleetHosted }),
}));
vi.mock("@/protoOS/store/hooks/useAuthRetry", () => {
  const retry = async ({
    request,
    onSuccess,
  }: {
    request: (params: object) => Promise<unknown>;
    onSuccess: (value: unknown) => unknown;
  }) => {
    await onSuccess(await request({}));
    if (mocks.changeConnectionAfterSuccess) useMinerStore.getState().resetDeviceData();
  };
  return { useAuthRetry: () => retry };
});

beforeEach(() => {
  vi.resetAllMocks();
  mocks.changeConnectionAfterSuccess = false;
  mocks.isFleetHosted = false;
  useMinerStore.getState().resetDeviceData();
  mocks.adapter.getContainerSettings.mockResolvedValue({
    supported: true,
    controls: [],
    setpoints: [{ name: "coolant_pump_speed_hz", value: 50, unit: "Hz" }],
  });
});
afterEach(() => {
  vi.useRealTimers();
});

test("does not refresh the old adapter when the connection changes after write success", async () => {
  const dismiss = vi.fn();
  render(<ContainerSystemControlsModal onDismiss={dismiss} />);
  const input = await screen.findByLabelText("Pump speed (Hz)");
  const generation = useMinerStore.getState().connectionGeneration;
  mocks.changeConnectionAfterSuccess = true;
  fireEvent.change(input, { target: { value: "45" } });
  fireEvent.keyDown(input, { key: "Enter" });
  await waitFor(() => expect(useMinerStore.getState().connectionGeneration).toBeGreaterThan(generation));
  await act(async () => {});
  expect(mocks.adapter.getCurrentTelemetry).not.toHaveBeenCalled();
  expect(dismiss).not.toHaveBeenCalled();
});

const breakerOnly = (closed?: boolean) => ({
  supported: false,
  controls: [],
  setpoints: [],
  mainCircuitBreaker: { closed },
});

test("shows the dedicated breaker when ordinary plant controls are unsupported", async () => {
  mocks.adapter.getContainerSettings.mockResolvedValue(breakerOnly(true));
  render(<ContainerSystemControlsModal onDismiss={vi.fn()} />);
  const breaker = await screen.findByRole("region", { name: "Main circuit breaker controls" });
  expect(within(breaker).getByText("Closed")).toBeInTheDocument();
  expect(within(breaker).getByLabelText("Main circuit breaker closed")).toBeChecked();
  expect(screen.queryByText("Controls are unavailable on this container.")).not.toBeInTheDocument();
  expect(screen.queryByText("No controls are available on this container.")).not.toBeInTheDocument();
});

test("hides the breaker unless the controller advertises it", async () => {
  mocks.adapter.getContainerSettings.mockResolvedValue({
    supported: true,
    controls: [{ name: "air_conditioner_1", enabled: true }],
    setpoints: [],
  });
  render(<ContainerSystemControlsModal onDismiss={vi.fn()} />);
  await screen.findByLabelText("Air conditioner 1 power");
  expect(screen.queryByRole("region", { name: "Main circuit breaker controls" })).not.toBeInTheDocument();
});

test("disables the breaker while its state is unknown", async () => {
  mocks.adapter.getContainerSettings.mockResolvedValue(breakerOnly(undefined));
  render(<ContainerSystemControlsModal onDismiss={vi.fn()} />);
  const breaker = await screen.findByRole("region", { name: "Main circuit breaker controls" });
  expect(within(breaker).getByText("Unavailable")).toBeInTheDocument();
  expect(within(breaker).getByLabelText("Main circuit breaker closed")).toBeDisabled();
});

test("confirms whole-container scope and writes only the dedicated breaker command", async () => {
  mocks.adapter.getContainerSettings.mockResolvedValueOnce(breakerOnly(true)).mockResolvedValue(breakerOnly(false));
  mocks.adapter.setMainCircuitBreakerState.mockResolvedValue(undefined);
  render(<ContainerSystemControlsModal onDismiss={vi.fn()} />);
  fireEvent.click(await screen.findByLabelText("Main circuit breaker closed"));

  const dialog = await screen.findByRole("dialog", { name: "Open the main circuit breaker?" });
  expect(within(dialog).getByText(/whole container, not a single tank or module/)).toBeInTheDocument();
  fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
  expect(mocks.adapter.setMainCircuitBreakerState).not.toHaveBeenCalled();

  fireEvent.click(screen.getByLabelText("Main circuit breaker closed"));
  fireEvent.click(await screen.findByRole("button", { name: "Open breaker" }));
  await waitFor(() => expect(screen.getByText("Open")).toBeInTheDocument());
  expect(mocks.adapter.setMainCircuitBreakerState).toHaveBeenCalledExactlyOnceWith(false, expect.anything());
  expect(mocks.adapter.setContainerControl).not.toHaveBeenCalled();
});

const confirmBreakerChange = async (action: string) => {
  fireEvent.click(await screen.findByLabelText("Main circuit breaker closed"));
  fireEvent.click(await screen.findByRole("button", { name: action }));
};

test("reports a failed breaker write without replaying it", async () => {
  // The controller closed the breaker even though the write call failed.
  mocks.adapter.getContainerSettings.mockResolvedValueOnce(breakerOnly(false)).mockResolvedValue(breakerOnly(true));
  mocks.adapter.setMainCircuitBreakerState.mockRejectedValue(new Error(BREAKER_OUTCOME_UNKNOWN));
  render(<ContainerSystemControlsModal onDismiss={vi.fn()} />);
  await confirmBreakerChange("Close breaker");

  const breaker = screen.getByRole("region", { name: "Main circuit breaker controls" });
  expect(await within(breaker).findByRole("alert")).toHaveTextContent(BREAKER_OUTCOME_UNKNOWN);
  // The state shown afterwards comes from a separate read, never a second write.
  expect(mocks.adapter.getContainerSettings).toHaveBeenCalledTimes(2);
  expect(mocks.adapter.setMainCircuitBreakerState).toHaveBeenCalledOnce();
  expect(within(breaker).getByText("Closed")).toBeInTheDocument();
});

test("reports a stalled breaker write as an unknown outcome without replaying it", async () => {
  mocks.adapter.getContainerSettings.mockResolvedValue(breakerOnly(false));
  render(<ContainerSystemControlsModal onDismiss={vi.fn()} />);
  const toggle = await screen.findByLabelText("Main circuit breaker closed");
  vi.useFakeTimers();
  mocks.adapter.setMainCircuitBreakerState.mockImplementation(() => new Promise(() => {}));
  fireEvent.click(toggle);
  fireEvent.click(screen.getByRole("button", { name: "Close breaker" }));
  await act(async () => {});

  await act(() => vi.advanceTimersByTimeAsync(30_000));
  const breaker = screen.getByRole("region", { name: "Main circuit breaker controls" });
  expect(within(breaker).getByRole("alert")).toHaveTextContent(BREAKER_OUTCOME_UNKNOWN);
  expect(mocks.adapter.setMainCircuitBreakerState).toHaveBeenCalledOnce();
});

test("keeps the breaker write outcome when the state cannot be re-read", async () => {
  mocks.adapter.getContainerSettings
    .mockResolvedValueOnce(breakerOnly(false))
    .mockRejectedValueOnce(new Error("Failed to fetch"));
  mocks.adapter.setMainCircuitBreakerState.mockRejectedValue(new Error(BREAKER_OUTCOME_UNKNOWN));
  render(<ContainerSystemControlsModal onDismiss={vi.fn()} />);
  await confirmBreakerChange("Close breaker");

  const breaker = screen.getByRole("region", { name: "Main circuit breaker controls" });
  const alert = await within(breaker).findByRole("alert");
  expect(alert).toHaveTextContent(BREAKER_OUTCOME_UNKNOWN);
  expect(alert).toHaveTextContent("The current state could not be re-read.");
  expect(within(breaker).getByText("Unavailable")).toBeInTheDocument();
  expect(mocks.adapter.setMainCircuitBreakerState).toHaveBeenCalledOnce();
});

test("does not report a confirmed breaker write as failed when the state cannot be re-read", async () => {
  mocks.adapter.getContainerSettings
    .mockResolvedValueOnce(breakerOnly(true))
    .mockRejectedValueOnce(new Error("Failed to fetch"));
  mocks.adapter.setMainCircuitBreakerState.mockResolvedValue(undefined);
  render(<ContainerSystemControlsModal onDismiss={vi.fn()} />);
  await confirmBreakerChange("Open breaker");

  const breaker = screen.getByRole("region", { name: "Main circuit breaker controls" });
  const alert = await within(breaker).findByRole("alert");
  expect(alert).toHaveTextContent("The controller accepted the change, but the current state could not be re-read.");
  expect(alert).not.toHaveTextContent("Could not save");
  expect(within(breaker).getByText("Unavailable")).toBeInTheDocument();
  expect(mocks.adapter.setMainCircuitBreakerState).toHaveBeenCalledOnce();
});

test("keeps the breaker read-only when the miner is hosted in Fleet", async () => {
  mocks.isFleetHosted = true;
  mocks.adapter.getContainerSettings.mockResolvedValue(breakerOnly(true));
  render(<ContainerSystemControlsModal onDismiss={vi.fn()} />);
  const breaker = await screen.findByRole("region", { name: "Main circuit breaker controls" });
  expect(within(breaker).getByText("Closed")).toBeInTheDocument();
  expect(within(breaker).getByLabelText("Main circuit breaker closed")).toBeDisabled();
  expect(within(breaker).getByText(/needs a direct connection to the controller/)).toBeInTheDocument();
});

test("shows plant alarms when the controller advertises alarm commands without plant controls", async () => {
  mocks.adapter.getContainerSettings.mockResolvedValue({
    supported: false,
    controls: [],
    setpoints: [],
    alarmMute: true,
    alarmReset: false,
  });
  render(<ContainerSystemControlsModal onDismiss={vi.fn()} />);
  expect(await screen.findByRole("region", { name: "Container alarms" })).toBeInTheDocument();
  expect(screen.queryByText("Controls are unavailable on this container.")).not.toBeInTheDocument();
  expect(screen.queryByText("No controls are available on this container.")).not.toBeInTheDocument();
});

test("hides plant alarms unless the controller advertises an alarm command", async () => {
  mocks.adapter.getContainerSettings.mockResolvedValue({
    supported: true,
    controls: [{ name: "air_conditioner_1", enabled: true }],
    setpoints: [],
    alarmMute: false,
    alarmReset: false,
  });
  render(<ContainerSystemControlsModal onDismiss={vi.fn()} />);
  await screen.findByLabelText("Air conditioner 1 power");
  expect(screen.queryByRole("region", { name: "Container alarms" })).not.toBeInTheDocument();
});
