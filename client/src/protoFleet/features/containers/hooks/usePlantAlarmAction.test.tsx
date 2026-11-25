import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { READBACK_SETTLE_MS } from "../plantAlarms";
import { usePlantAlarmAction } from "./usePlantAlarmAction";
import { ContainerSchema, PlantAlarmAction } from "@/protoFleet/api/generated/containers/v1/containers_pb";

const api = vi.hoisted(() => ({ runPlantAlarmAction: vi.fn(), getEquipmentCommandResult: vi.fn() }));
vi.mock("@/protoFleet/api/clients", () => ({ containersClient: api }));

const stamp = new Date();
const container = (
  plant: Record<string, unknown> = { alarmActive: true, buzzerActive: true },
  { at = stamp, canControl = true, controls = {} as Record<string, unknown> } = {},
) =>
  create(ContainerSchema, {
    deviceIdentifier: "ct1",
    canControlEquipment: canControl,
    controls: {
      supported: true,
      observedAt: timestampFromDate(stamp),
      alarmMuteSupported: true,
      alarmResetSupported: true,
      ...controls,
    },
    plant: { observedAt: timestampFromDate(at), ...plant },
  });
const later = (plant: Record<string, unknown>) => container(plant, { at: new Date(stamp.getTime() + 1) });
const finished = {
  result: {
    status: "finished",
    successCount: 1,
    failureCount: 0,
    deviceResults: [{ updatedAt: timestampFromDate(stamp) }],
  },
};
const setup = (initial = container()) => {
  const refresh = vi.fn();
  const hook = renderHook(({ value }) => usePlantAlarmAction("ct1", value, refresh), {
    initialProps: { value: initial },
  });
  return { ...hook, refresh };
};
const submitOnce = async (result: ReturnType<typeof setup>["result"], action: "mute" | "reset") => {
  await act(async () => {
    void result.current.submit(action);
    void result.current.submit(action);
  });
};

afterEach(() => {
  vi.resetAllMocks();
  vi.useRealTimers();
});

describe("plant alarm actions", () => {
  it("sends mute once and confirms only from a newer observation with the buzzer silent", async () => {
    api.runPlantAlarmAction.mockResolvedValue({ batchIdentifier: "batch" });
    api.getEquipmentCommandResult.mockResolvedValue(finished);
    const { result, rerender } = setup();
    await submitOnce(result, "mute");
    expect(api.runPlantAlarmAction).toHaveBeenCalledExactlyOnceWith(
      { deviceIdentifier: "ct1", action: PlantAlarmAction.MUTE },
      { timeoutMs: 10_000 },
    );
    await waitFor(() => expect(result.current.status).toContain("report the result"));
    expect(result.current.busy).toBe(true);
    // Same observation time: not newer than completion.
    rerender({ value: container({ alarmActive: true, buzzerActive: false }) });
    expect(result.current.busy).toBe(true);
    // Newer but the buzzer is still sounding.
    rerender({ value: later({ alarmActive: true, buzzerActive: true }) });
    expect(result.current.busy).toBe(true);
    rerender({
      value: container({ alarmActive: true, buzzerActive: false }, { at: new Date(stamp.getTime() + 2) }),
    });
    await waitFor(() => expect(result.current.busy).toBe(false));
    expect(result.current.notice).toMatchObject({ tone: "success" });
    expect(result.current.notice?.text).toContain("buzzer silenced");
    expect(api.runPlantAlarmAction).toHaveBeenCalledTimes(1);
  });

  it("confirms reset only when no alarm is active and critical is not set", async () => {
    api.runPlantAlarmAction.mockResolvedValue({ batchIdentifier: "batch" });
    api.getEquipmentCommandResult.mockResolvedValue(finished);
    const { result, rerender } = setup();
    await submitOnce(result, "reset");
    expect(api.runPlantAlarmAction).toHaveBeenCalledExactlyOnceWith(
      { deviceIdentifier: "ct1", action: PlantAlarmAction.RESET },
      { timeoutMs: 10_000 },
    );
    await waitFor(() => expect(result.current.status).toContain("report the result"));
    rerender({ value: later({ alarmActive: false, criticalAlarmActive: false }) });
    await waitFor(() => expect(result.current.busy).toBe(false));
    expect(result.current.notice).toMatchObject({ tone: "success", text: "Alarms cleared." });
  });

  it("reports a reset that leaves alarms active as a warning, not a failure", async () => {
    api.runPlantAlarmAction.mockResolvedValue({ batchIdentifier: "batch" });
    api.getEquipmentCommandResult.mockResolvedValue(finished);
    const { result, rerender } = setup();
    await submitOnce(result, "reset");
    await waitFor(() => expect(result.current.status).toContain("report the result"));
    // Right after completion the controller may not have cleared latched alarms yet.
    rerender({ value: later({ alarmActive: false, criticalAlarmActive: true }) });
    expect(result.current.busy).toBe(true);
    rerender({
      value: container(
        { alarmActive: false, criticalAlarmActive: true },
        { at: new Date(stamp.getTime() + READBACK_SETTLE_MS) },
      ),
    });
    await waitFor(() => expect(result.current.busy).toBe(false));
    expect(result.current.notice).toMatchObject({
      tone: "warning",
      text: "Reset sent, but the controller still reports active alarms. An alarm whose cause is still present stays active.",
    });
  });

  it("judges only observations newer than the one shown when the command was sent", async () => {
    api.runPlantAlarmAction.mockResolvedValue({ batchIdentifier: "batch" });
    api.getEquipmentCommandResult.mockResolvedValue(finished);
    // The controller clock runs ahead of Fleet: the observation on screen is stamped after completion.
    const shown = new Date(stamp.getTime() + 30_000);
    const { result, rerender } = setup(container({ alarmActive: true, buzzerActive: true }, { at: shown }));
    await submitOnce(result, "mute");
    await waitFor(() => expect(result.current.status).toContain("report the result"));
    rerender({
      value: container({ alarmActive: true, buzzerActive: false }, { at: new Date(stamp.getTime() + 10_000) }),
    });
    expect(result.current.busy).toBe(true);
    rerender({ value: container({ alarmActive: true, buzzerActive: false }, { at: new Date(shown.getTime() + 1) }) });
    await waitFor(() => expect(result.current.busy).toBe(false));
    expect(result.current.notice).toMatchObject({ tone: "success" });
  });

  it("says mute cannot be confirmed when the controller does not report the buzzer", async () => {
    api.runPlantAlarmAction.mockResolvedValue({ batchIdentifier: "batch" });
    api.getEquipmentCommandResult.mockResolvedValue(finished);
    const { result, rerender } = setup();
    await submitOnce(result, "mute");
    await waitFor(() => expect(result.current.status).toContain("report the result"));
    rerender({ value: later({ alarmActive: true }) });
    await waitFor(() => expect(result.current.busy).toBe(false));
    expect(result.current.notice).toMatchObject({
      tone: "warning",
      text: "Mute sent. This controller does not report buzzer state, so the result cannot be confirmed.",
    });
  });

  it("warns when a reading after the settle window still shows the buzzer sounding", async () => {
    api.runPlantAlarmAction.mockResolvedValue({ batchIdentifier: "batch" });
    api.getEquipmentCommandResult.mockResolvedValue(finished);
    const { result, rerender } = setup();
    await submitOnce(result, "mute");
    await waitFor(() => expect(result.current.status).toContain("report the result"));
    rerender({ value: later({ alarmActive: true, buzzerActive: true }) });
    expect(result.current.busy).toBe(true);
    rerender({
      value: container(
        { alarmActive: true, buzzerActive: true },
        { at: new Date(stamp.getTime() + READBACK_SETTLE_MS) },
      ),
    });
    await waitFor(() => expect(result.current.busy).toBe(false));
    expect(result.current.notice).toMatchObject({
      tone: "warning",
      text: "Mute sent, but the controller still reports the buzzer sounding.",
    });
  });

  it("does not treat an unreported critical flag as clear after a reset", async () => {
    api.runPlantAlarmAction.mockResolvedValue({ batchIdentifier: "batch" });
    api.getEquipmentCommandResult.mockResolvedValue(finished);
    const { result, rerender } = setup();
    await submitOnce(result, "reset");
    await waitFor(() => expect(result.current.status).toContain("report the result"));
    rerender({ value: later({ alarmActive: false }) });
    await waitFor(() => expect(result.current.busy).toBe(false));
    expect(result.current.notice).toMatchObject({
      tone: "warning",
      text: "Reset sent. This controller does not report alarm state, so the result cannot be confirmed.",
    });
  });

  it("stays unconfirmed without a newer observation and never resubmits", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(stamp);
    api.runPlantAlarmAction.mockResolvedValue({ batchIdentifier: "batch" });
    api.getEquipmentCommandResult.mockResolvedValue(finished);
    const { result, rerender } = setup();
    await submitOnce(result, "mute");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000);
    });
    expect(result.current.status).toContain("report the result");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(121_000);
    });
    expect(result.current.busy).toBe(false);
    expect(result.current.notice).toMatchObject({ tone: "warning" });
    expect(result.current.notice?.text).toContain("no newer controller reading arrived");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300_000);
    });
    expect(api.runPlantAlarmAction).toHaveBeenCalledTimes(1);
    // A later observation settles the open notice instead of leaving it stuck.
    rerender({ value: later({ alarmActive: true, buzzerActive: false }) });
    expect(result.current.notice).toMatchObject({ tone: "success" });
    expect(api.runPlantAlarmAction).toHaveBeenCalledTimes(1);
  });

  it("bounds a hung command result request and reports the outcome as unknown", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(stamp);
    api.runPlantAlarmAction.mockResolvedValue({ batchIdentifier: "batch" });
    api.getEquipmentCommandResult.mockReturnValue(new Promise(() => undefined));
    const { result } = setup();
    await submitOnce(result, "reset");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(121_000);
    });
    expect(result.current.busy).toBe(false);
    expect(result.current.notice).toMatchObject({
      tone: "error",
      text: "Command outcome unknown. Check the alarm state before trying again.",
    });
    expect(api.runPlantAlarmAction).toHaveBeenCalledTimes(1);
  });

  it("shows the backend failure message as-is and does not replay", async () => {
    api.runPlantAlarmAction.mockResolvedValue({ batchIdentifier: "batch" });
    api.getEquipmentCommandResult.mockResolvedValue({
      result: {
        status: "finished",
        successCount: 0,
        failureCount: 1,
        deviceResults: [{ errorMessage: "Controller is not the primary; nothing was changed." }],
      },
    });
    const { result } = setup();
    await submitOnce(result, "mute");
    await waitFor(() =>
      expect(result.current.notice?.text).toBe("Controller is not the primary; nothing was changed."),
    );
    expect(result.current.notice?.tone).toBe("error");
    expect(api.runPlantAlarmAction).toHaveBeenCalledTimes(1);
  });

  it("separates a refused request from one whose outcome is unknown", async () => {
    api.runPlantAlarmAction.mockRejectedValueOnce(new ConnectError("Alarm reset is not supported", Code.Unimplemented));
    const { result } = setup();
    await submitOnce(result, "reset");
    await waitFor(() => expect(result.current.notice?.text).toBe("Reset not sent. Alarm reset is not supported"));
    api.runPlantAlarmAction.mockRejectedValueOnce(new ConnectError("deadline exceeded", Code.DeadlineExceeded));
    await submitOnce(result, "mute");
    await waitFor(() =>
      expect(result.current.notice?.text).toBe(
        "Mute outcome unknown: deadline exceeded. Check the alarm state before trying again.",
      ),
    );
    expect(api.getEquipmentCommandResult).not.toHaveBeenCalled();
  });

  it.each([
    ["no permission", container({ alarmActive: true }, { canControl: false })],
    ["capability not advertised", container({ alarmActive: true }, { controls: { alarmMuteSupported: false } })],
    ["capability unknown", container({ alarmActive: true }, { controls: { alarmMuteSupported: undefined } })],
    ["controls stale", container({ alarmActive: true }, { controls: { stale: true } })],
  ])("does not submit with %s", async (_name, value) => {
    const { result } = setup(value);
    await submitOnce(result, "mute");
    expect(api.runPlantAlarmAction).not.toHaveBeenCalled();
    expect(result.current.busy).toBe(false);
  });
});
