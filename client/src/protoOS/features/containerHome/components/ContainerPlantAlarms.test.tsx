import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import ContainerPlantAlarms from "./ContainerPlantAlarms";
import PlantAlarmOutcomeNotice from "./PlantAlarmOutcomeNotice";
import { ALARM_MUTE_OUTCOME_UNKNOWN, ALARM_RESET_OUTCOME_UNKNOWN } from "@/protoOS/api/adapters/alarmMessages";
import { toCanonicalContainer } from "@/protoOS/api/adapters/mdkV2/normalize";
import { beginStoreWrite } from "@/protoOS/store/storeWriteGuard";
import useMinerStore from "@/protoOS/store/useMinerStore";

const mocks = vi.hoisted(() => ({
  isFleetHosted: false,
  telemetry: [] as { enabled?: boolean; pollIntervalMs?: number }[],
  adapter: { muteContainerAlarm: vi.fn(), resetContainerAlarm: vi.fn() } as Record<string, unknown>,
}));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: () => ({ adapter: mocks.adapter, isFleetHosted: mocks.isFleetHosted }),
}));
vi.mock("@/protoOS/api/hooks/useTelemetry", () => ({
  useTelemetry: (props: { enabled?: boolean; pollIntervalMs?: number }) => mocks.telemetry.push(props),
}));
// The real useAuthRetry runs; only the token refresh is stubbed, and it always succeeds.
vi.mock("@/protoOS/store/hooks/useAuth", () => ({
  useAuthErrors: () => ({
    handleAuthErrors: ({
      error,
      onError,
      onSuccess,
    }: {
      error: { status?: number };
      onError?: (error: unknown) => void;
      onSuccess?: () => unknown;
    }) => (error?.status === 401 ? onSuccess?.() : onError?.(error)),
  }),
}));

const START = Date.parse("2026-11-14T12:00:00Z");
const map = { containerMap: { tanks: [{ id: 1 }] } };
const plantAlarms = {
  warning: {
    id: "coolant-low-pressure",
    name: "Coolant low pressure",
    severity: "SEVERITY_WARNING",
    componentId: "pump-1",
  },
  critical: { id: "tank-b-over-temp", name: "Tank B over temperature", severity: "SEVERITY_CRITICAL" },
};

interface Observation {
  at?: number;
  alarms?: (typeof plantAlarms)[keyof typeof plantAlarms][];
  alarmActive?: boolean;
  criticalAlarmActive?: boolean;
  buzzerActive?: boolean;
  alarmMuteSupported?: boolean;
}
// Merges a plant observation through the real normalizer, as a poll would. A poll's request is
// started when it is sent: pass an earlier `request` for a reading fetched before that point.
const startRequest = () => beginStoreWrite("telemetry:current:test");
const observe = (
  { at = START, alarms = [], alarmMuteSupported, ...flags }: Observation,
  request: ReturnType<typeof startRequest> = startRequest(),
) =>
  act(() => {
    const fixture = toCanonicalContainer(
      map,
      {},
      { timestamp: new Date(at).toISOString(), alarms, alarmMuteSupported, plant: flags } as never,
      "ct1-test",
    );
    const store = useMinerStore.getState();
    store.hardware.replaceHardware(fixture.hardware);
    store.telemetry.mergeTelemetry(fixture.telemetry, request.requestId);
  });
const active = (extra: Observation = {}): Observation => ({
  alarms: [plantAlarms.warning, plantAlarms.critical],
  alarmActive: true,
  criticalAlarmActive: true,
  buzzerActive: true,
  ...extra,
});

const settings = { alarmMute: true, alarmReset: true };
const renderAlarms = (value: Partial<typeof settings> = settings) =>
  render(<ContainerPlantAlarms settings={{ ...settings, ...value }} />);
const button = (name: string) => screen.getByRole("button", { name });
const confirm = async (open: string, confirmName = open) => {
  fireEvent.click(button(open));
  const dialog = screen.getByRole("dialog");
  fireEvent.click(within(dialog).getByRole("button", { name: confirmName }));
  await act(async () => {});
};
const lastTelemetry = () => mocks.telemetry[mocks.telemetry.length - 1];
const advance = (ms: number) => act(() => vi.advanceTimersByTimeAsync(ms));

beforeEach(() => {
  vi.resetAllMocks();
  vi.useFakeTimers({ toFake: ["setInterval", "clearInterval", "setTimeout", "clearTimeout", "Date"] });
  vi.setSystemTime(START);
  mocks.isFleetHosted = false;
  mocks.telemetry = [];
  mocks.adapter = {
    muteContainerAlarm: vi.fn().mockResolvedValue(undefined),
    resetContainerAlarm: vi.fn().mockResolvedValue(undefined),
  };
  useMinerStore.getState().resetDeviceData();
});
afterEach(() => {
  vi.useRealTimers();
});

describe("evidence", () => {
  test("lists active alarms and shows the flags and freshness before any action", async () => {
    await observe(active({ at: START - 12_000 }));
    renderAlarms();
    const section = screen.getByRole("region", { name: "Container alarms" });
    expect(within(section).getByText("Coolant low pressure")).toBeInTheDocument();
    expect(within(section).getByText("Warning · pump-1")).toBeInTheDocument();
    expect(within(section).getByText("Tank B over temperature")).toBeInTheDocument();
    expect(within(section).getByText("Critical")).toBeInTheDocument();
    expect(within(section).getByText("Sounding")).toBeInTheDocument();
    expect(within(section).getAllByText("Active")).toHaveLength(2);
    expect(within(section).getByText("Observed 12s ago")).toBeInTheDocument();
    expect(button("Mute buzzer")).toBeEnabled();
    expect(button("Reset alarms")).toBeEnabled();
  });

  test("marks an old observation stale", async () => {
    await observe(active({ at: START - 2 * 60_000 }));
    renderAlarms();
    expect(screen.getByText("Observed 2m ago · Stale")).toBeInTheDocument();
  });

  test("offers no actions when nothing is active and the buzzer is silent", async () => {
    await observe({ alarmActive: false, criticalAlarmActive: false, buzzerActive: false });
    renderAlarms();
    expect(screen.getByText("No active alarms.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Mute buzzer" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Reset alarms" })).not.toBeInTheDocument();
  });

  test("treats unreported flags as unknown, never as clear", async () => {
    await observe({});
    renderAlarms();
    expect(screen.getAllByText("Unknown")).toHaveLength(3);
    expect(screen.getByText("Alarm state not reported.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Mute buzzer" })).not.toBeInTheDocument();
  });

  test("offers the actions for a sounding buzzer without a listed alarm", async () => {
    await observe({ alarmActive: false, buzzerActive: true });
    renderAlarms();
    expect(button("Mute buzzer")).toBeEnabled();
  });

  test("says when an active alarm is not listed", async () => {
    await observe({ alarmActive: true, buzzerActive: false });
    renderAlarms();
    expect(screen.getByText("The controller reports an active alarm but does not list it.")).toBeInTheDocument();
  });
});

describe("capability", () => {
  test("reads reset as unsupported on real CT1 hardware while mute stays available", async () => {
    await observe(active());
    renderAlarms({ alarmReset: false });
    expect(button("Mute buzzer")).toBeEnabled();
    expect(button("Reset alarms")).toBeDisabled();
    expect(
      screen.getByText(
        "This controller does not support resetting alarms. Alarms clear on their own once their cause clears.",
      ),
    ).toBeInTheDocument();
  });

  test("disables mute when the controller does not advertise it", async () => {
    await observe(active());
    renderAlarms({ alarmMute: false });
    expect(button("Mute buzzer")).toBeDisabled();
    expect(screen.getByText("This controller does not support muting alarms.")).toBeInTheDocument();
  });

  test("honors live health narrowing of mute", async () => {
    await observe(active({ alarmMuteSupported: false }));
    renderAlarms();
    expect(button("Mute buzzer")).toBeDisabled();
    expect(screen.getByText("The controller cannot mute alarms right now.")).toBeInTheDocument();
    expect(button("Reset alarms")).toBeEnabled();
  });

  test("keeps assuming mute when health does not report support", async () => {
    await observe(active({ alarmMuteSupported: undefined }));
    renderAlarms();
    expect(button("Mute buzzer")).toBeEnabled();
  });

  test("keeps showing evidence but disables both actions in a Fleet-hosted session", async () => {
    mocks.isFleetHosted = true;
    await observe(active());
    renderAlarms();
    expect(screen.getByText("Coolant low pressure")).toBeInTheDocument();
    expect(button("Mute buzzer")).toBeDisabled();
    expect(button("Reset alarms")).toBeDisabled();
    // The reason is shared by both actions, so it shows once under the button row.
    expect(screen.getAllByText("Muting or resetting alarms needs a direct connection to the controller.")).toHaveLength(
      1,
    );
    fireEvent.click(button("Mute buzzer"));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(mocks.adapter.muteContainerAlarm).not.toHaveBeenCalled();
  });
});

describe("confirmation", () => {
  test("distinguishes mute from reset and says neither fixes a hardware fault", async () => {
    await observe(active());
    renderAlarms();
    fireEvent.click(button("Mute buzzer"));
    let dialog = screen.getByRole("dialog", { name: "Mute alarm buzzer?" });
    expect(
      within(dialog).getByText(/Silences the alarm buzzer\. Alarms stay active until their cause clears\./),
    ).toBeInTheDocument();
    expect(within(dialog).getByText(/does not fix or clear a hardware fault/)).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(mocks.adapter.muteContainerAlarm).not.toHaveBeenCalled();

    fireEvent.click(button("Reset alarms"));
    dialog = screen.getByRole("dialog", { name: "Reset alarms?" });
    expect(
      within(dialog).getByText(
        /Asks the controller to clear latched alarms\. An alarm whose cause is still present stays active\./,
      ),
    ).toBeInTheDocument();
    expect(within(dialog).getByText(/does not fix or clear a hardware fault/)).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(mocks.adapter.resetContainerAlarm).not.toHaveBeenCalled();
  });

  test("sends only the confirmed action, once", async () => {
    await observe(active());
    renderAlarms();
    await confirm("Mute buzzer");
    expect(mocks.adapter.muteContainerAlarm).toHaveBeenCalledOnce();
    expect(mocks.adapter.resetContainerAlarm).not.toHaveBeenCalled();
  });
});

describe("readback", () => {
  test("waits for the controller after a mute and polls quickly meanwhile", async () => {
    await observe(active({ at: START - 1_000 }));
    renderAlarms();
    expect(lastTelemetry()?.enabled).toBe(false);
    await confirm("Mute buzzer");
    expect(screen.getByRole("status")).toHaveTextContent("Sent; waiting for the controller to report…");
    expect(lastTelemetry()).toMatchObject({ enabled: true, pollIntervalMs: 3_000 });
    expect(button("Mute buzzer")).toBeDisabled();
  });

  test("confirms a mute from the next observation showing the buzzer silent", async () => {
    await observe(active({ at: START - 1_000 }));
    renderAlarms();
    await confirm("Mute buzzer");
    await advance(2_000);
    // The same observation is not new: it cannot confirm anything.
    await observe(active({ at: START - 1_000, buzzerActive: false }));
    expect(screen.getByRole("status")).toHaveTextContent("Sent; waiting for the controller to report…");
    await observe(active({ at: START + 1_000, buzzerActive: false }));
    expect(screen.getByRole("status")).toHaveTextContent("Buzzer muted. Alarms stay active until their cause clears.");
    expect(screen.getByText("Coolant low pressure")).toBeInTheDocument();
    expect(lastTelemetry()?.enabled).toBe(false);
    expect(mocks.adapter.muteContainerAlarm).toHaveBeenCalledOnce();
  });

  test("does not use the browser clock to judge whether an observation is newer", async () => {
    // The controller clock runs well behind the browser: its observations still compare to each other.
    await observe(active({ at: START - 600_000 }));
    renderAlarms();
    await confirm("Mute buzzer");
    await observe(active({ at: START - 590_000, buzzerActive: false }));
    expect(screen.getByRole("status")).toHaveTextContent("Buzzer muted.");
  });

  test("reports a buzzer that is still sounding only after the settle window", async () => {
    await observe(active({ at: START - 1_000 }));
    renderAlarms();
    await confirm("Mute buzzer");
    await observe(active({ at: START + 1_000 }));
    expect(screen.getByRole("status")).toHaveTextContent("Sent; waiting");
    await advance(6_000);
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Mute sent, but the controller still reports the buzzer sounding.",
    );
    expect(lastTelemetry()?.enabled).toBe(false);
    // The first decisive result is kept: a later reading cannot say what this command did.
    await observe(active({ at: START + 9_000, buzzerActive: false }));
    expect(screen.getByRole("alert")).toHaveTextContent("still reports the buzzer sounding");
  });

  test("keeps a confirmed mute when an unrelated alarm sounds the buzzer later", async () => {
    await observe(active({ at: START - 1_000 }));
    renderAlarms();
    await confirm("Mute buzzer");
    await observe(active({ at: START + 1_000, buzzerActive: false }));
    expect(screen.getByRole("status")).toHaveTextContent("Buzzer muted.");
    await advance(10_000);
    await observe(active({ at: START + 11_000, buzzerActive: true }));
    expect(screen.getByRole("status")).toHaveTextContent("Buzzer muted.");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  test("does not confirm from a reading requested before the command was acknowledged", async () => {
    await observe(active({ at: START - 1_000 }));
    let finish: () => void = () => {};
    mocks.adapter.muteContainerAlarm = vi.fn(() => new Promise<void>((resolve) => (finish = resolve)));
    renderAlarms();
    await confirm("Mute buzzer");
    // A poll sent while the write was in flight lands after the acknowledgement.
    const inFlight = startRequest();
    await act(async () => finish());
    await observe(active({ at: START + 500, buzzerActive: false }), inFlight);
    expect(screen.getByRole("status")).toHaveTextContent("Sent; waiting");
    await observe(active({ at: START + 1_000, buzzerActive: false }));
    expect(screen.getByRole("status")).toHaveTextContent("Buzzer muted.");
  });

  test("cannot confirm a mute when the controller does not report the buzzer", async () => {
    await observe(active({ at: START - 1_000, buzzerActive: undefined, alarmActive: true }));
    renderAlarms();
    await confirm("Mute buzzer");
    await observe(active({ at: START + 1_000, buzzerActive: undefined }));
    await advance(6_000);
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Mute sent. This controller does not report buzzer state, so the result cannot be confirmed.",
    );
  });

  test("confirms a reset when a newer observation shows no alarms", async () => {
    await observe(active({ at: START - 1_000 }));
    renderAlarms();
    await confirm("Reset alarms");
    await observe({ at: START + 1_000, alarmActive: false, criticalAlarmActive: false, buzzerActive: false });
    expect(screen.getByRole("status")).toHaveTextContent("Alarms reset. The controller reports no active alarms.");
    expect(screen.queryByRole("button", { name: "Reset alarms" })).not.toBeInTheDocument();
    expect(mocks.adapter.resetContainerAlarm).toHaveBeenCalledOnce();
  });

  test("does not count an unreported critical flag as clear after a reset", async () => {
    await observe(active({ at: START - 1_000 }));
    renderAlarms();
    await confirm("Reset alarms");
    await observe({ at: START + 1_000, alarmActive: false, buzzerActive: false });
    expect(screen.queryByText(/Alarms reset\./)).not.toBeInTheDocument();
    await advance(6_000);
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Reset sent. This controller does not report alarm state, so the result cannot be confirmed.",
    );
  });

  test("reports alarms that remain after a reset without calling it a failure", async () => {
    await observe(active({ at: START - 1_000 }));
    renderAlarms();
    await confirm("Reset alarms");
    await observe({
      at: START + 1_000,
      alarms: [plantAlarms.critical],
      alarmActive: true,
      criticalAlarmActive: true,
      buzzerActive: false,
    });
    await advance(6_000);
    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent(
      "Reset sent, but the controller still reports 1 active alarm. An alarm whose cause is still present stays active.",
    );
    expect(alert).not.toHaveTextContent(ALARM_RESET_OUTCOME_UNKNOWN);
    expect(screen.getByText("Tank B over temperature")).toBeInTheDocument();
  });

  test("keeps an unconfirmed result when no newer observation arrives, until dismissed", async () => {
    await observe(active({ at: START - 1_000 }));
    renderAlarms();
    await confirm("Mute buzzer");
    await advance(119_000);
    expect(screen.getByRole("status")).toHaveTextContent("Sent; waiting");
    await advance(2_000);
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Mute sent, but the controller has not reported since. Check the current alarm state before trying again.",
    );
    expect(lastTelemetry()?.enabled).toBe(false);
    // Nothing is replayed, and the operator may decide to try again.
    expect(mocks.adapter.muteContainerAlarm).toHaveBeenCalledOnce();
    expect(button("Mute buzzer")).toBeEnabled();
    fireEvent.click(button("Dismiss"));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});

describe("failures", () => {
  test("shows a refusal as unchanged, keeps it visible, and never replays", async () => {
    await observe(active());
    mocks.adapter.muteContainerAlarm = vi.fn().mockRejectedValue(
      Object.assign(new Error("The controller refused the request: not primary. The alarm state was not changed."), {
        writeOutcome: "refused",
      }),
    );
    renderAlarms();
    await confirm("Mute buzzer");
    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent(
      "The controller refused the request: not primary. The alarm state was not changed.",
    );
    expect(alert).not.toHaveTextContent("did not confirm");
    await advance(30_000);
    expect(screen.getByRole("alert")).toBe(alert);
    expect(mocks.adapter.muteContainerAlarm).toHaveBeenCalledOnce();
    expect(lastTelemetry()?.enabled).toBe(false);
    fireEvent.click(button("Dismiss"));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  test.each([
    ["mute", "Mute buzzer", "muteContainerAlarm", ALARM_MUTE_OUTCOME_UNKNOWN],
    ["reset", "Reset alarms", "resetContainerAlarm", ALARM_RESET_OUTCOME_UNKNOWN],
  ])("shows an unknown %s outcome as not confirmed and never replays", async (_name, label, method, unknown) => {
    await observe(active());
    mocks.adapter[method] = vi.fn().mockRejectedValue(Object.assign(new Error(unknown), { writeOutcome: "unknown" }));
    renderAlarms();
    await confirm(label);
    expect(screen.getByRole("alert")).toHaveTextContent(unknown);
    await advance(60_000);
    expect(screen.getByRole("alert")).toHaveTextContent(unknown);
    expect(mocks.adapter[method]).toHaveBeenCalledOnce();
  });

  test("treats an unclassified failure as an unknown outcome", async () => {
    await observe(active());
    mocks.adapter.muteContainerAlarm = vi.fn().mockRejectedValue(new TypeError("Failed to fetch"));
    renderAlarms();
    await confirm("Mute buzzer");
    expect(screen.getByRole("alert")).toHaveTextContent(ALARM_MUTE_OUTCOME_UNKNOWN);
  });

  test("treats a stalled request as an unknown outcome", async () => {
    await observe(active());
    mocks.adapter.muteContainerAlarm = vi.fn(() => new Promise(() => {}));
    renderAlarms();
    await confirm("Mute buzzer");
    await advance(30_000);
    expect(screen.getByRole("alert")).toHaveTextContent(ALARM_MUTE_OUTCOME_UNKNOWN);
    expect(mocks.adapter.muteContainerAlarm).toHaveBeenCalledOnce();
  });

  test("re-sends once after a sign-in rejection and reports a second rejection as not sent", async () => {
    await observe(active());
    mocks.adapter.muteContainerAlarm = vi.fn().mockRejectedValue({ status: 401, error: { code: 16 } });
    renderAlarms();
    await confirm("Mute buzzer");
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Sign in again before trying again. The alarm state was not changed.",
    );
    expect(mocks.adapter.muteContainerAlarm).toHaveBeenCalledTimes(2);
  });

  test("sends the command again after a token refresh when the first was rejected for sign-in", async () => {
    await observe(active({ at: START - 1_000 }));
    mocks.adapter.muteContainerAlarm = vi
      .fn()
      .mockRejectedValueOnce({ status: 401, error: { code: 16 } })
      .mockResolvedValueOnce(undefined);
    renderAlarms();
    await confirm("Mute buzzer");
    expect(mocks.adapter.muteContainerAlarm).toHaveBeenCalledTimes(2);
    expect(screen.getByRole("status")).toHaveTextContent("Sent; waiting");
  });

  test("never re-sends a failure that is not a sign-in rejection", async () => {
    await observe(active());
    mocks.adapter.muteContainerAlarm = vi.fn().mockRejectedValue({ status: 500, error: { code: 13 } });
    renderAlarms();
    await confirm("Mute buzzer");
    expect(screen.getByRole("alert")).toHaveTextContent(ALARM_MUTE_OUTCOME_UNKNOWN);
    expect(mocks.adapter.muteContainerAlarm).toHaveBeenCalledOnce();
  });

  test("keeps the host open while a command is in flight", async () => {
    await observe(active());
    let finish: () => void = () => {};
    mocks.adapter.muteContainerAlarm = vi.fn(() => new Promise<void>((resolve) => (finish = resolve)));
    const busy = vi.fn();
    render(<ContainerPlantAlarms settings={settings} onBusyChange={busy} />);
    fireEvent.click(button("Mute buzzer"));
    await act(async () => {});
    expect(busy).toHaveBeenLastCalledWith(true);
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Mute buzzer" }));
    await act(async () => {});
    expect(busy).toHaveBeenLastCalledWith(true);
    finish();
    await act(async () => {});
    expect(busy).toHaveBeenLastCalledWith(false);
  });
  test("keeps an unknown outcome visible after the section is closed and reopened", async () => {
    await observe(active());
    mocks.adapter.muteContainerAlarm = vi.fn().mockRejectedValue(new TypeError("Failed to fetch"));
    const first = renderAlarms();
    await confirm("Mute buzzer");
    first.unmount();
    renderAlarms();
    expect(screen.getByRole("alert")).toHaveTextContent(ALARM_MUTE_OUTCOME_UNKNOWN);
    expect(mocks.adapter.muteContainerAlarm).toHaveBeenCalledOnce();
  });

  test("records a write that finishes after the section closes, without aborting it", async () => {
    await observe(active({ at: START - 1_000 }));
    let finish: () => void = () => {};
    let signal: AbortSignal | undefined;
    mocks.adapter.muteContainerAlarm = vi.fn(
      (params?: { signal?: AbortSignal }) =>
        new Promise<void>((resolve) => {
          signal = params?.signal;
          finish = resolve;
        }),
    );
    const first = renderAlarms();
    await confirm("Mute buzzer");
    first.unmount();
    expect(signal?.aborted).toBe(false);
    await act(async () => finish());
    renderAlarms();
    expect(screen.getByRole("status")).toHaveTextContent("Sent; waiting");
    expect(button("Mute buzzer")).toBeDisabled();
  });

  test("shows the outcome beside the controls while the section is closed, and settles it there", async () => {
    await observe(active({ at: START - 1_000 }));
    const first = renderAlarms();
    await confirm("Mute buzzer");
    first.unmount();
    render(<PlantAlarmOutcomeNotice />);
    expect(screen.getByRole("status")).toHaveTextContent("Alarm command sent; waiting for the controller to report…");
    await observe(active({ at: START + 1_000, buzzerActive: false }));
    expect(screen.getByRole("status")).toHaveTextContent("Buzzer muted.");
    fireEvent.click(button("Dismiss"));
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  test("forgets the outcome when the controller connection changes", async () => {
    await observe(active());
    mocks.adapter.muteContainerAlarm = vi.fn().mockRejectedValue(new TypeError("Failed to fetch"));
    const first = renderAlarms();
    await confirm("Mute buzzer");
    first.unmount();
    act(() => useMinerStore.getState().resetDeviceData());
    await observe(active());
    renderAlarms();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});
