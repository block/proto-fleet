import { type ComponentProps, useState } from "react";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { usePlantAlarmAction } from "../hooks/usePlantAlarmAction";
import type { PlantAlarmItem } from "../plantAlarms";
import PlantAlarmsPanel from "./PlantAlarmsPanel";
import { ContainerSchema, PlantAlarmAction } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { Severity } from "@/protoFleet/api/generated/errors/v1/errors_pb";

const api = vi.hoisted(() => ({ runPlantAlarmAction: vi.fn(), getEquipmentCommandResult: vi.fn() }));
vi.mock("@/protoFleet/api/clients", () => ({ containersClient: api }));

const secondsAgo = (seconds: number) => timestampFromDate(new Date(Date.now() - seconds * 1000));
const container = (
  options: {
    plant?: Record<string, unknown> | null;
    controls?: Record<string, unknown>;
    canControl?: boolean;
  } = {},
) =>
  create(ContainerSchema, {
    deviceIdentifier: "ct1",
    canControlEquipment: options.canControl ?? true,
    controls: {
      supported: true,
      observedAt: secondsAgo(5),
      alarmMuteSupported: true,
      alarmResetSupported: false,
      ...options.controls,
    },
    plant:
      options.plant === null
        ? undefined
        : {
            observedAt: secondsAgo(12),
            alarmActive: true,
            criticalAlarmActive: false,
            buzzerActive: true,
            ...options.plant,
          },
  });
const idle = { busy: false, submit: vi.fn(), dismiss: vi.fn() };
const alarm = (overrides: Partial<PlantAlarmItem> = {}): PlantAlarmItem => ({
  id: "e1",
  name: "Coolant level low",
  severity: Severity.MAJOR,
  confirmedAt: new Date(Date.now() - 30_000),
  retained: false,
  ...overrides,
});
const renderPanel = (props: Partial<ComponentProps<typeof PlantAlarmsPanel>> = {}) =>
  render(<PlantAlarmsPanel container={container()} alarms={[]} command={idle} {...props} />);

afterEach(() => vi.resetAllMocks());

describe("plant alarms evidence", () => {
  it("shows flags, freshness and individual alarms before any action", () => {
    renderPanel({ alarms: [alarm()] });
    const panel = screen.getByRole("region", { name: "Container alarms" });
    expect(within(panel).getByText("Observed just now")).toBeInTheDocument();
    expect(within(panel).getByText("Sounding")).toBeInTheDocument();
    expect(within(panel).getByText("Coolant level low")).toBeInTheDocument();
    expect(within(panel).getByText(/Major/)).toBeInTheDocument();
    const order = [
      panel.querySelector("dl"),
      panel.querySelector("ul"),
      screen.getByRole("button", { name: "Mute buzzer" }),
    ];
    expect(order[0]!.compareDocumentPosition(order[1]!) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(order[1]!.compareDocumentPosition(order[2]!) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("never reads a missing flag as clear", () => {
    renderPanel({
      container: container({
        plant: { alarmActive: undefined, criticalAlarmActive: undefined, buzzerActive: undefined },
      }),
    });
    expect(screen.getAllByText("Not reported")).toHaveLength(3);
    expect(screen.queryByText("Clear")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Mute buzzer" })).not.toBeInTheDocument();
  });

  it("marks carried alarms with the time they were last confirmed", () => {
    renderPanel({
      alarms: [
        alarm({ retained: true, confirmedAt: new Date(Date.now() - 10 * 60_000) }),
        alarm({ id: "e2", name: "Fan fault" }),
      ],
    });
    expect(screen.getByText(/Last confirmed 10m ago/)).toBeInTheDocument();
    expect(screen.getByText(/Last seen just now/)).toBeInTheDocument();
  });

  it("marks stale observations and disables actions", () => {
    const stale = container({ plant: { observedAt: secondsAgo(300) } });
    renderPanel({ container: stale });
    expect(screen.getByText("Out of date. Last observed 5m ago")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Mute buzzer" })).toBeDisabled();
    expect(screen.getByText("Actions are unavailable while the alarm state is out of date.")).toBeInTheDocument();
  });

  it("treats the plant stale flag as out of date", () => {
    renderPanel({ container: container({ plant: { stale: true } }) });
    expect(screen.getByRole("button", { name: "Mute buzzer" })).toBeDisabled();
  });

  it("keeps the flags and says details are unavailable when the alarm list cannot be read", () => {
    renderPanel({ alarmsFailed: true });
    expect(screen.getByText("Alarm details unavailable")).toBeInTheDocument();
    expect(screen.getByText("Sounding")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Mute buzzer" })).toBeEnabled();
  });
});

describe("plant alarm actions gating", () => {
  it("offers no action without an active alarm or sounding buzzer", () => {
    renderPanel({
      container: container({ plant: { alarmActive: false, criticalAlarmActive: false, buzzerActive: false } }),
    });
    expect(screen.getByText("Silent")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Mute buzzer" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Reset alarms" })).not.toBeInTheDocument();
  });

  it("offers a sounding buzzer even when alarm flags are clear", () => {
    renderPanel({ container: container({ plant: { alarmActive: false, buzzerActive: true } }) });
    expect(screen.getByRole("button", { name: "Mute buzzer" })).toBeEnabled();
  });

  it("offers only mute on a controller that cannot reset, and says why", () => {
    renderPanel();
    expect(screen.getByRole("button", { name: "Mute buzzer" })).toBeEnabled();
    expect(screen.queryByRole("button", { name: "Reset alarms" })).not.toBeInTheDocument();
    expect(screen.getByText("This controller clears alarms on its own once their cause clears.")).toBeInTheDocument();
  });

  it("offers both when both are advertised", () => {
    renderPanel({ container: container({ controls: { alarmResetSupported: true } }) });
    expect(screen.getByRole("button", { name: "Mute buzzer" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Reset alarms" })).toBeEnabled();
  });

  it("hides actions without equipment control permission", () => {
    renderPanel({ container: container({ canControl: false, controls: { alarmResetSupported: true } }) });
    expect(screen.getByText("Sounding")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Mute buzzer" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Reset alarms" })).not.toBeInTheDocument();
    expect(screen.getByText(/needs permission to control equipment/)).toBeInTheDocument();
  });

  it("disables actions when the controls observation is stale or old", () => {
    const { unmount } = renderPanel({ container: container({ controls: { stale: true } }) });
    expect(screen.getByRole("button", { name: "Mute buzzer" })).toBeDisabled();
    expect(screen.getByText(/until the controller reports its alarm controls/)).toBeInTheDocument();
    unmount();
    renderPanel({ container: container({ controls: { observedAt: secondsAgo(300) } }) });
    expect(screen.getByRole("button", { name: "Mute buzzer" })).toBeDisabled();
  });

  it("disables actions while one is in flight or the container could not be refreshed", () => {
    const { unmount } = renderPanel({ command: { ...idle, busy: true, status: "Sending mute…" } });
    expect(screen.getByRole("button", { name: "Mute buzzer" })).toBeDisabled();
    expect(screen.getByRole("status")).toHaveTextContent("Sending mute…");
    unmount();
    renderPanel({ refreshFailed: true });
    expect(screen.getByRole("button", { name: "Mute buzzer" })).toBeDisabled();
  });
});

function Harness({ initial, canControl = true }: { initial: ReturnType<typeof container>; canControl?: boolean }) {
  const [current, setCurrent] = useState(initial);
  const command = usePlantAlarmAction("ct1", current, () => undefined);
  return (
    <>
      <PlantAlarmsPanel container={current} alarms={[]} command={command} />
      <button
        onClick={() =>
          setCurrent(
            container({
              plant: { observedAt: timestampFromDate(new Date(Date.now() + 1000)), buzzerActive: false },
              canControl,
            }),
          )
        }
      >
        new observation
      </button>
    </>
  );
}

describe("plant alarm confirmation and outcome", () => {
  const finished = {
    result: {
      status: "finished",
      successCount: 1,
      failureCount: 0,
      deviceResults: [{ updatedAt: timestampFromDate(new Date()) }],
    },
  };

  it("confirms mute with its own copy, sends once and reports the readback", async () => {
    api.runPlantAlarmAction.mockResolvedValue({ batchIdentifier: "b" });
    api.getEquipmentCommandResult.mockResolvedValue(finished);
    render(<Harness initial={container({ controls: { alarmResetSupported: true } })} />);
    fireEvent.click(screen.getByRole("button", { name: "Mute buzzer" }));
    const dialog = screen.getByTestId("plant-alarm-mute-dialog");
    expect(within(dialog).getByText("Mute alarm buzzer?")).toBeInTheDocument();
    expect(
      within(dialog).getByText("Silences the alarm buzzer. Alarms stay active until their cause clears."),
    ).toBeInTheDocument();
    expect(within(dialog).getByText("Muting does not fix or clear a hardware fault.")).toBeInTheDocument();
    expect(api.runPlantAlarmAction).not.toHaveBeenCalled();
    fireEvent.click(within(dialog).getByRole("button", { name: "Mute buzzer" }));
    await waitFor(() =>
      expect(screen.getByRole("status")).toHaveTextContent("Waiting for the controller to report the result"),
    );
    expect(api.runPlantAlarmAction).toHaveBeenCalledExactlyOnceWith(
      { deviceIdentifier: "ct1", action: PlantAlarmAction.MUTE },
      { timeoutMs: 10_000 },
    );
    expect(screen.getByRole("button", { name: "Mute buzzer" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Reset alarms" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "new observation" }));
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("Alarm buzzer silenced."));
    expect(api.runPlantAlarmAction).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
    expect(screen.queryByText(/Alarm buzzer silenced/)).not.toBeInTheDocument();
  });

  it("confirms reset with its own copy and does nothing on cancel", () => {
    render(<Harness initial={container({ controls: { alarmResetSupported: true } })} />);
    fireEvent.click(screen.getByRole("button", { name: "Reset alarms" }));
    const dialog = screen.getByTestId("plant-alarm-reset-dialog");
    expect(within(dialog).getByText("Reset alarms?")).toBeInTheDocument();
    expect(
      within(dialog).getByText(
        "Asks the controller to clear latched alarms. An alarm whose cause is still present stays active.",
      ),
    ).toBeInTheDocument();
    expect(within(dialog).getByText("Resetting does not fix or clear a hardware fault.")).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(screen.queryByTestId("plant-alarm-reset-dialog")).not.toBeInTheDocument();
    expect(api.runPlantAlarmAction).not.toHaveBeenCalled();
  });

  it("keeps a failure visible until dismissed", async () => {
    api.runPlantAlarmAction.mockResolvedValue({ batchIdentifier: "b" });
    api.getEquipmentCommandResult.mockResolvedValue({
      result: {
        status: "finished",
        successCount: 0,
        failureCount: 1,
        deviceResults: [{ errorMessage: "PLC refused the command." }],
      },
    });
    render(<Harness initial={container()} />);
    fireEvent.click(screen.getByRole("button", { name: "Mute buzzer" }));
    fireEvent.click(within(screen.getByTestId("plant-alarm-mute-dialog")).getByRole("button", { name: "Mute buzzer" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("PLC refused the command.");
    expect(screen.getByRole("button", { name: "Mute buzzer" })).toBeEnabled();
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});
