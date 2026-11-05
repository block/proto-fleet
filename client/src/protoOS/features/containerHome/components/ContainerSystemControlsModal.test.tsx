import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";
import ContainerSystemControlsModal from "./ContainerSystemControlsModal";
import useMinerStore from "@/protoOS/store/useMinerStore";

const mocks = vi.hoisted(() => ({
  changeConnectionAfterSuccess: false,
  adapter: { getContainerSettings: vi.fn(), setContainerSetpoint: vi.fn(), getCurrentTelemetry: vi.fn() },
}));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({ useMinerHosting: () => ({ adapter: mocks.adapter }) }));
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
  useMinerStore.getState().resetDeviceData();
  mocks.adapter.getContainerSettings.mockResolvedValue({
    supported: true,
    controls: [],
    setpoints: [{ name: "coolant_pump_speed_hz", value: 50, unit: "Hz" }],
  });
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
