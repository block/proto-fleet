import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import ContainerSystemControlsModal from "./ContainerSystemControlsModal";
import { FAN_REVERSAL_OUTCOME_UNKNOWN } from "@/protoOS/api/adapters/fanReversalMessages";
import type { ContainerSettings, DryCoolerFanReversalStatus } from "@/protoOS/api/adapters/types";
import { asComponentId, asContainerId } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";

const mocks = vi.hoisted(() => ({
  isFleetHosted: false,
  adapter: {
    getContainerSettings: vi.fn(),
    setContainerControl: vi.fn(),
    setContainerSetpoint: vi.fn(),
    runDryCoolerFanReversal: vi.fn(),
    getCurrentTelemetry: vi.fn(),
  },
}));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: () => ({ adapter: mocks.adapter, isFleetHosted: mocks.isFleetHosted }),
}));
// The real useAuthRetry runs; only the token refresh is stubbed, and it always succeeds. The stub
// is one stable object, as the real hook's result is, so effects that depend on it do not re-run.
const auth = vi.hoisted(() => ({
  handleAuthErrors: ({
    error,
    onError,
    onSuccess,
  }: {
    error: { status?: number };
    onError?: (error: unknown) => void;
    onSuccess?: () => unknown;
  }) => (error?.status === 401 ? onSuccess?.() : onError?.(error)),
}));
vi.mock("@/protoOS/store/hooks/useAuth", () => ({ useAuthErrors: () => auth }));

const settings = (status?: DryCoolerFanReversalStatus, auto = true): ContainerSettings => ({
  supported: true,
  controls: [
    { name: "dry_cooler", enabled: true },
    { name: "dry_cooler_auto", enabled: auto },
  ],
  setpoints: [],
  dryCoolerFanReversal: { status },
});
const standby = settings({ state: "stopped", durationMinutes: 7 });

beforeEach(() => {
  vi.resetAllMocks();
  mocks.isFleetHosted = false;
  useMinerStore.getState().resetDeviceData();
  useMinerStore.setState((state) => {
    for (const slot of [1, 2, 3]) {
      const id = asComponentId(`ct1:fan:${slot}`);
      state.hardware.fans.set(id, {
        ref: { id, kind: "fan", owner: { kind: "container", id: asContainerId("ct1") } },
        slot,
      });
    }
  });
  mocks.adapter.getContainerSettings.mockResolvedValue(standby);
});
afterEach(() => {
  vi.useRealTimers();
});

const section = () => screen.getByRole("region", { name: "Fan reversal" });
const openConfirmation = async () => {
  fireEvent.click(await screen.findByRole("button", { name: "Run fan reversal" }));
  return screen.findByRole("dialog", { name: "Run fan reversal for 7 minutes?" });
};

test("names the duration and the affected fans before sending one dedicated command", async () => {
  mocks.adapter.getContainerSettings
    .mockResolvedValueOnce(standby)
    .mockResolvedValue(settings({ state: "finished", durationMinutes: 7 }));
  let finish!: (status: DryCoolerFanReversalStatus) => void;
  mocks.adapter.runDryCoolerFanReversal.mockImplementation(() => new Promise((resolve) => (finish = resolve)));
  render(<ContainerSystemControlsModal onDismiss={vi.fn()} />);
  expect(await screen.findByTestId("fan-reversal-state")).toHaveTextContent("Stopped");

  const dialog = await openConfirmation();
  expect(dialog).toHaveTextContent("Affects all 3 dry cooler fans: they run in reverse for 7 minutes");
  expect(dialog).toHaveTextContent("does not change the dry cooler's power, Auto or Manual mode, or speed");
  fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
  expect(mocks.adapter.runDryCoolerFanReversal).not.toHaveBeenCalled();

  const confirm = await openConfirmation();
  fireEvent.click(within(confirm).getByRole("button", { name: "Run fan reversal" }));
  expect(await within(section()).findByRole("status")).toHaveTextContent("Fan reversal sent for 7 min");
  // The PLC owns the cooler while the procedure runs.
  expect(screen.getByLabelText("Dry cooler power")).toBeDisabled();
  expect(screen.getByText("Dry cooler settings are locked while the fan reversal runs.")).toBeInTheDocument();

  await act(async () => finish({ state: "finished", durationMinutes: 7 }));
  expect(
    await within(section()).findByText("The controller reports the fan reversal finished after 7 minutes."),
  ).toBeInTheDocument();
  expect(mocks.adapter.runDryCoolerFanReversal).toHaveBeenCalledExactlyOnceWith(7, expect.anything());
  expect(mocks.adapter.setContainerControl).not.toHaveBeenCalled();
  expect(screen.getByLabelText("Dry cooler power")).toBeEnabled();
});

test("reports a controller refusal and sends nothing else", async () => {
  const refusal = Object.assign(
    new Error(
      "The controller refused the request: a dry-cooler fan reversal is already in progress. The fan reversal did not start.",
    ),
    { writeOutcome: "refused" },
  );
  mocks.adapter.runDryCoolerFanReversal.mockRejectedValue(refusal);
  render(<ContainerSystemControlsModal onDismiss={vi.fn()} />);
  fireEvent.click(within(await openConfirmation()).getByRole("button", { name: "Run fan reversal" }));

  expect(await within(section()).findByRole("alert")).toHaveTextContent(refusal.message);
  expect(mocks.adapter.runDryCoolerFanReversal).toHaveBeenCalledOnce();
  expect(screen.getByRole("button", { name: "Run fan reversal" })).toBeEnabled();
});

test("reports a run that stopped before finishing as failed", async () => {
  mocks.adapter.runDryCoolerFanReversal.mockResolvedValue({ state: "stopped", durationMinutes: 7 });
  render(<ContainerSystemControlsModal onDismiss={vi.fn()} />);
  fireEvent.click(within(await openConfirmation()).getByRole("button", { name: "Run fan reversal" }));
  expect(await within(section()).findByRole("alert")).toHaveTextContent(
    "The fan reversal stopped before the controller reported it finished.",
  );
});

test("holds another run after an unknown outcome until a newer reading, and never resends", async () => {
  mocks.adapter.runDryCoolerFanReversal.mockRejectedValue(
    Object.assign(new Error(FAN_REVERSAL_OUTCOME_UNKNOWN), { writeOutcome: "unknown" }),
  );
  render(<ContainerSystemControlsModal onDismiss={vi.fn()} />);
  const dialog = await openConfirmation();
  vi.useFakeTimers({ shouldAdvanceTime: true });
  fireEvent.click(within(dialog).getByRole("button", { name: "Run fan reversal" }));

  expect(await within(section()).findByRole("alert")).toHaveTextContent(FAN_REVERSAL_OUTCOME_UNKNOWN);
  expect(screen.getByRole("button", { name: "Run fan reversal" })).toBeDisabled();
  expect(screen.getByText("Waiting for a new controller reading before another run.")).toBeInTheDocument();
  // The PLC may still be running the procedure, so the cooler stays locked on the same evidence.
  expect(screen.getByLabelText("Dry cooler power")).toBeDisabled();

  await act(() => vi.advanceTimersByTimeAsync(3_000));
  await waitFor(() => expect(screen.getByRole("button", { name: "Run fan reversal" })).toBeEnabled());
  expect(screen.getByLabelText("Dry cooler power")).toBeEnabled();
  expect(mocks.adapter.runDryCoolerFanReversal).toHaveBeenCalledOnce();
});

test("re-sends once after a sign-in rejection that the token refresh recovers", async () => {
  mocks.adapter.runDryCoolerFanReversal
    .mockRejectedValueOnce({ status: 401, error: { code: 16 } })
    .mockResolvedValueOnce({ state: "finished", durationMinutes: 7 });
  render(<ContainerSystemControlsModal onDismiss={vi.fn()} />);
  fireEvent.click(within(await openConfirmation()).getByRole("button", { name: "Run fan reversal" }));
  expect(await within(section()).findByText(/finished after 7 minutes/)).toBeInTheDocument();
  expect(mocks.adapter.runDryCoolerFanReversal).toHaveBeenCalledTimes(2);
});

test("reports a second sign-in rejection as not started", async () => {
  mocks.adapter.runDryCoolerFanReversal.mockRejectedValue({ status: 401, error: { code: 16 } });
  render(<ContainerSystemControlsModal onDismiss={vi.fn()} />);
  fireEvent.click(within(await openConfirmation()).getByRole("button", { name: "Run fan reversal" }));
  expect(await within(section()).findByRole("alert")).toHaveTextContent(
    "Sign in again before trying again. The fan reversal did not start.",
  );
  expect(mocks.adapter.runDryCoolerFanReversal).toHaveBeenCalledTimes(2);
});

test("never re-sends a failure that is not a sign-in rejection", async () => {
  mocks.adapter.runDryCoolerFanReversal.mockRejectedValue({ status: 500, error: { code: 13 } });
  render(<ContainerSystemControlsModal onDismiss={vi.fn()} />);
  fireEvent.click(within(await openConfirmation()).getByRole("button", { name: "Run fan reversal" }));
  expect(await within(section()).findByRole("alert")).toHaveTextContent(FAN_REVERSAL_OUTCOME_UNKNOWN);
  expect(mocks.adapter.runDryCoolerFanReversal).toHaveBeenCalledOnce();
});

test("keeps reading until the controller reports a settled state", async () => {
  mocks.adapter.getContainerSettings.mockResolvedValueOnce(settings({ state: "unknown" })).mockResolvedValue(standby);
  vi.useFakeTimers({ shouldAdvanceTime: true });
  render(<ContainerSystemControlsModal onDismiss={vi.fn()} />);
  expect(await screen.findByTestId("fan-reversal-state")).toHaveTextContent("Unknown");
  expect(screen.getByRole("button", { name: "Run fan reversal" })).toBeDisabled();

  await act(() => vi.advanceTimersByTimeAsync(3_000));
  await waitFor(() => expect(screen.getByTestId("fan-reversal-state")).toHaveTextContent("Stopped"));
  expect(screen.getByRole("button", { name: "Run fan reversal" })).toBeEnabled();
});

test("keeps holding another run after the unknown outcome is dismissed", async () => {
  mocks.adapter.runDryCoolerFanReversal.mockRejectedValue(
    Object.assign(new Error(FAN_REVERSAL_OUTCOME_UNKNOWN), { writeOutcome: "unknown" }),
  );
  render(<ContainerSystemControlsModal onDismiss={vi.fn()} />);
  const dialog = await openConfirmation();
  vi.useFakeTimers({ shouldAdvanceTime: true });
  fireEvent.click(within(dialog).getByRole("button", { name: "Run fan reversal" }));
  await within(section()).findByRole("alert");

  fireEvent.click(within(section()).getByRole("button", { name: "Dismiss" }));
  expect(within(section()).queryByRole("alert")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Run fan reversal" })).toBeDisabled();
  expect(screen.getByText("Waiting for a new controller reading before another run.")).toBeInTheDocument();

  await act(() => vi.advanceTimersByTimeAsync(3_000));
  await waitFor(() => expect(screen.getByRole("button", { name: "Run fan reversal" })).toBeEnabled());
  expect(mocks.adapter.runDryCoolerFanReversal).toHaveBeenCalledOnce();
});

test("does not release the hold on a reading requested before the run settled", async () => {
  let fail!: (error: unknown) => void;
  mocks.adapter.runDryCoolerFanReversal.mockImplementation(() => new Promise((_resolve, reject) => (fail = reject)));
  render(<ContainerSystemControlsModal onDismiss={vi.fn()} />);
  const dialog = await openConfirmation();
  vi.useFakeTimers({ shouldAdvanceTime: true });
  fireEvent.click(within(dialog).getByRole("button", { name: "Run fan reversal" }));
  await within(section()).findByRole("status");

  // A poll goes out while the run is open and is answered only after the run settles.
  let answerPoll!: (value: ContainerSettings) => void;
  mocks.adapter.getContainerSettings.mockImplementationOnce(() => new Promise((resolve) => (answerPoll = resolve)));
  await act(() => vi.advanceTimersByTimeAsync(3_000));
  await act(async () => fail(Object.assign(new Error(FAN_REVERSAL_OUTCOME_UNKNOWN), { writeOutcome: "unknown" })));
  await within(section()).findByRole("alert");
  await act(async () => answerPoll(standby));

  expect(screen.getByRole("button", { name: "Run fan reversal" })).toBeDisabled();
  expect(screen.getByText("Waiting for a new controller reading before another run.")).toBeInTheDocument();
  await act(() => vi.advanceTimersByTimeAsync(3_000));
  await waitFor(() => expect(screen.getByRole("button", { name: "Run fan reversal" })).toBeEnabled());
});

test("holds a run while a System controls change is being saved", async () => {
  mocks.adapter.setContainerControl.mockImplementation(() => new Promise(() => {}));
  render(<ContainerSystemControlsModal onDismiss={vi.fn()} />);
  await screen.findByTestId("fan-reversal-state");
  fireEvent.click(screen.getByLabelText("Dry cooler power"));
  fireEvent.click(
    within(await screen.findByRole("dialog", { name: /^Turn off/ })).getByRole("button", { name: "Turn off" }),
  );

  await waitFor(() => expect(screen.getByRole("button", { name: "Run fan reversal" })).toBeDisabled());
  expect(screen.getByText("Wait for the current setting to save.")).toBeInTheDocument();
  expect(mocks.adapter.runDryCoolerFanReversal).not.toHaveBeenCalled();
});

test("warns when the dry cooler mode reads differently after the run", async () => {
  mocks.adapter.getContainerSettings
    .mockResolvedValueOnce(standby)
    .mockResolvedValue(settings({ state: "finished", durationMinutes: 7 }, false));
  mocks.adapter.runDryCoolerFanReversal.mockResolvedValue({ state: "finished", durationMinutes: 7 });
  render(<ContainerSystemControlsModal onDismiss={vi.fn()} />);
  fireEvent.click(within(await openConfirmation()).getByRole("button", { name: "Run fan reversal" }));
  expect(await within(section()).findByRole("alert")).toHaveTextContent(
    "The dry cooler mode now reads Manual; it was Auto before the run.",
  );
  // The form shows the mode the controller reads now, so the next change starts from it.
  await waitFor(() => expect(screen.getByLabelText("Mode")).toHaveTextContent("Manual"));
});

test("keeps the procedure read-only while the controller reports it running", async () => {
  mocks.adapter.getContainerSettings.mockResolvedValue(settings({ state: "inProgress", durationMinutes: 4 }));
  render(<ContainerSystemControlsModal onDismiss={vi.fn()} />);
  expect(await screen.findByTestId("fan-reversal-state")).toHaveTextContent("Running · 4 min");
  expect(screen.getByRole("button", { name: "Run fan reversal" })).toBeDisabled();
  expect(screen.getByText("The controller reports a fan reversal running.")).toBeInTheDocument();
  expect(screen.getByLabelText("Dry cooler power")).toBeDisabled();
});

test("shows the readback but no command when the miner is hosted in Fleet", async () => {
  mocks.isFleetHosted = true;
  render(<ContainerSystemControlsModal onDismiss={vi.fn()} />);
  expect(await screen.findByTestId("fan-reversal-state")).toHaveTextContent("Stopped");
  expect(screen.getByRole("button", { name: "Run fan reversal" })).toBeDisabled();
  expect(screen.getByText(/needs a direct connection to the controller/)).toBeInTheDocument();
});

test("hides the procedure unless the controller advertises it", async () => {
  mocks.adapter.getContainerSettings.mockResolvedValue({ ...standby, dryCoolerFanReversal: undefined });
  render(<ContainerSystemControlsModal onDismiss={vi.fn()} />);
  await screen.findByLabelText("Dry cooler power");
  expect(screen.queryByRole("region", { name: "Fan reversal" })).not.toBeInTheDocument();
});
