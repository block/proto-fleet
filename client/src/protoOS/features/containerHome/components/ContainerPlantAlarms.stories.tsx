import { type ComponentType, useEffect, useMemo } from "react";
import { expect, userEvent, waitFor, within } from "storybook/test";
import ContainerPlantAlarms from "./ContainerPlantAlarms";
import { MdkV2Adapter, type MdkV2Client } from "@/protoOS/api/adapters/mdkV2";
import { toCanonicalContainer } from "@/protoOS/api/adapters/mdkV2/normalize";
import MinerHostingContext from "@/protoOS/contexts/MinerHostingContext/MinerHostingContext";
import useMinerStore from "@/protoOS/store/useMinerStore";

type Outcome = "accepted" | "unknown" | "refused";

interface PreviewProps {
  /** Real CT1 hardware cannot reset alarms: it advertises mute only. */
  resetSupported?: boolean;
  hosted?: boolean;
  /** Start with every alarm cleared and the buzzer silent. */
  quiet?: boolean;
  /** How the controller answers a command. */
  outcome?: Outcome;
}

const warning = {
  id: "coolant-low-pressure",
  name: "Coolant low pressure",
  severity: "SEVERITY_WARNING",
  componentId: "coolant-pump",
};
const critical = {
  id: "tank-b-outlet-over-temperature",
  name: "Tank B outlet over temperature",
  severity: "SEVERITY_CRITICAL",
  componentId: "tank-b",
};
const map = { containerMap: { tanks: [{ id: 1 }] } };

interface ControllerState {
  alarms: (typeof warning)[];
  buzzer: boolean;
}

// The observation time moves on with each poll, as a live controller's does.
const observation = (state: ControllerState) => ({
  timestamp: new Date(Date.now() - 2_000).toISOString(),
  alarms: state.alarms,
  alarmMuteSupported: true,
  plant: {
    alarmActive: state.alarms.length > 0,
    criticalAlarmActive: state.alarms.some((alarm) => alarm === critical),
    buzzerActive: state.buzzer,
  },
});

function Preview({ resetSupported = false, hosted = false, quiet = false, outcome = "accepted" }: PreviewProps) {
  const preview = useMemo(() => {
    // A small controller: mute silences the buzzer, reset clears alarms whose cause is gone.
    const state: ControllerState = { alarms: quiet ? [] : [warning, critical], buzzer: !quiet };
    const health = () => observation(state);
    const command = (apply: () => void) => async () => {
      if (outcome === "unknown") throw { status: 500, error: { code: 13, message: "plc timeout" } };
      if (outcome === "refused")
        throw { status: 400, error: { code: 9, message: "this node is not the control primary" } };
      apply();
      return { data: {} };
    };
    const client = {
      hashboardServiceGetContainerMap: async () => ({ data: map }),
      miningServiceGetMiningStatus: async () => ({ data: {} }),
      telemetryServiceGetContainerHealth: async () => ({ data: health() }),
      powerServiceMuteContainerAlarm: command(() => {
        state.buzzer = false;
      }),
      powerServiceResetContainerAlarm: command(() => {
        state.alarms = state.alarms.filter((alarm) => alarm === critical);
        if (!state.alarms.length) state.buzzer = false;
      }),
    };
    return { adapter: new MdkV2Adapter(client as unknown as MdkV2Client, { deviceId: "ct1-story" }), health };
  }, [quiet, outcome]);
  useEffect(() => {
    const fixture = toCanonicalContainer(map, {}, preview.health() as never, "ct1-story");
    const store = useMinerStore.getState();
    store.resetDeviceData();
    store.hardware.replaceHardware(fixture.hardware);
    store.telemetry.mergeTelemetry(fixture.telemetry);
    return () => useMinerStore.getState().resetDeviceData();
  }, [preview]);
  return (
    <MinerHostingContext.Provider
      value={{
        adapter: preview.adapter,
        resolution: { status: "ready", adapterId: "mdk-v2", adapter: preview.adapter },
        minerRoot: "",
        closeButton: null,
        mode: hosted ? "fleet" : "direct",
        metadata: {},
      }}
    >
      <div className="max-w-xl bg-surface-base p-6 text-text-primary">
        <ContainerPlantAlarms settings={{ alarmMute: true, alarmReset: resetSupported }} />
      </div>
    </MinerHostingContext.Provider>
  );
}

const run =
  (label: string, confirmLabel = label) =>
  async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await userEvent.click(await within(canvasElement).findByRole("button", { name: label }));
    const dialog = await within(document.body).findByRole("dialog");
    await userEvent.click(within(dialog).getByRole("button", { name: confirmLabel }));
  };

/** The CT1 default: mute is available and reset is unsupported by the hardware. */
export const ActiveAlarmsMuteOnly = () => <Preview />;
export const MuteAndResetSupported = () => <Preview resetSupported />;
export const NoActiveAlarms = () => <Preview quiet />;
export const FleetHosted = () => <Preview resetSupported hosted />;

export const ConfirmMute = () => <Preview resetSupported />;
ConfirmMute.play = async ({ canvasElement }: { canvasElement: HTMLElement }) => {
  await userEvent.click(await within(canvasElement).findByRole("button", { name: "Mute buzzer" }));
};

export const ConfirmReset = () => <Preview resetSupported />;
ConfirmReset.play = async ({ canvasElement }: { canvasElement: HTMLElement }) => {
  await userEvent.click(await within(canvasElement).findByRole("button", { name: "Reset alarms" }));
};

/** Sent, and the next controller report has not arrived yet. */
export const MutedWaitingForController = () => <Preview />;
MutedWaitingForController.play = run("Mute buzzer");

/** The next controller report shows the buzzer silent. */
export const MuteConfirmed = () => <Preview />;
MuteConfirmed.play = async (context: { canvasElement: HTMLElement }) => {
  await run("Mute buzzer")(context);
  await waitFor(() => expect(within(context.canvasElement).getByText(/Buzzer muted/)).toBeInTheDocument(), {
    timeout: 10_000,
  });
};

/** Alarms whose cause is still present stay active after a reset. */
export const ResetLeavesActiveAlarm = () => <Preview resetSupported />;
ResetLeavesActiveAlarm.play = async (context: { canvasElement: HTMLElement }) => {
  await run("Reset alarms")(context);
  await waitFor(
    () => expect(within(context.canvasElement).getByText(/still reports 1 active alarm/)).toBeInTheDocument(),
    {
      timeout: 15_000,
    },
  );
};

/** The command may have reached the controller, so the operator must check the state. */
export const UnknownOutcome = () => <Preview outcome="unknown" />;
UnknownOutcome.play = run("Mute buzzer");

/** Nothing was written. */
export const Refused = () => <Preview outcome="refused" />;
Refused.play = run("Mute buzzer");

export default {
  title: "Proto OS/Containers/Home/Container alarms",
  component: ContainerPlantAlarms as ComponentType,
  parameters: { withRouter: false },
};
