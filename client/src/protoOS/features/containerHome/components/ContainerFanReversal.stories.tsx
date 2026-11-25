import { type ComponentType, useEffect, useMemo, useState } from "react";
import { userEvent, within } from "storybook/test";
import { resetFanReversalOutcome } from "../hooks/useFanReversalOutcome";
import ContainerFanReversal from "./ContainerFanReversal";
import FanReversalOutcomeNotice from "./FanReversalOutcomeNotice";
import { FAN_REVERSAL_OUTCOME_UNKNOWN } from "@/protoOS/api/adapters/fanReversalMessages";
import type { ContainerSettings, DryCoolerFanReversalStatus, MinerAdapter } from "@/protoOS/api/adapters/types";
import MinerHostingContext from "@/protoOS/contexts/MinerHostingContext/MinerHostingContext";

/** How the controller answers a run. `open` never answers, as during a real run. */
type Answer = "finished" | "stopped" | "refused" | "unknown" | "open";

interface PreviewProps {
  state?: DryCoolerFanReversalStatus["state"];
  answer?: Answer;
  hosted?: boolean;
}

const settingsFor = (state: DryCoolerFanReversalStatus["state"]): ContainerSettings => ({
  supported: true,
  controls: [{ name: "dry_cooler_auto", enabled: true }],
  setpoints: [],
  dryCoolerFanReversal: { status: { state, durationMinutes: 5 } },
});

function Preview({ state = "stopped", answer = "finished", hosted = false }: PreviewProps) {
  const [requestedAtMs] = useState(() => Date.now());
  const adapter = useMemo(() => {
    let current = state;
    return {
      getContainerSettings: async () => settingsFor(current),
      runDryCoolerFanReversal: async (durationMinutes: number) => {
        if (answer === "refused")
          throw Object.assign(
            new Error(
              "The controller refused the request: this node is not the container control primary. The fan reversal did not start.",
            ),
            { writeOutcome: "refused" },
          );
        if (answer === "unknown")
          throw Object.assign(new Error(FAN_REVERSAL_OUTCOME_UNKNOWN), { writeOutcome: "unknown" });
        if (answer === "open") {
          current = "inProgress";
          return new Promise<DryCoolerFanReversalStatus>(() => {});
        }
        current = answer;
        return { state: answer, durationMinutes };
      },
    } as unknown as MinerAdapter;
  }, [state, answer]);
  useEffect(() => {
    resetFanReversalOutcome();
    return resetFanReversalOutcome;
  }, [adapter]);
  return (
    <MinerHostingContext.Provider
      value={{
        adapter,
        resolution: { status: "ready", adapterId: "mdk-v2", adapter },
        minerRoot: "",
        closeButton: null,
        mode: hosted ? "fleet" : "direct",
        metadata: {},
      }}
    >
      <div className="max-w-xl space-y-6 bg-surface-base p-6 text-text-primary">
        <ContainerFanReversal settings={settingsFor(state)} settingsRequestedAtMs={requestedAtMs} dryCoolerAuto />
        {/* The notice shown beside System controls while the modal is closed. */}
        <FanReversalOutcomeNotice />
      </div>
    </MinerHostingContext.Provider>
  );
}

const open = async ({ canvasElement }: { canvasElement: HTMLElement }) => {
  await userEvent.click(await within(canvasElement).findByRole("button", { name: "Run fan reversal" }));
};
const run = async (context: { canvasElement: HTMLElement }) => {
  await open(context);
  const dialog = await within(document.body).findByRole("dialog");
  await userEvent.click(within(dialog).getByRole("button", { name: "Run fan reversal" }));
};

export const Standby = () => <Preview />;

export const Confirmation = () => <Preview />;
Confirmation.play = open;

/** The call stays open until the controller reports the procedure ended. */
export const Running = () => <Preview answer="open" />;
Running.play = run;

export const Finished = () => <Preview />;
Finished.play = run;

/** Nothing started. */
export const Refused = () => <Preview answer="refused" />;
Refused.play = run;

export const StoppedEarly = () => <Preview answer="stopped" />;
StoppedEarly.play = run;

/** The run may or may not have started; another run waits for a newer controller reading. */
export const UnknownOutcome = () => <Preview answer="unknown" />;
UnknownOutcome.play = run;

/** A run another client started. */
export const ControllerReportsRunning = () => <Preview state="inProgress" />;

export const FleetHosted = () => <Preview hosted />;

export default {
  title: "Proto OS/Containers/Home/Fan reversal",
  component: ContainerFanReversal as ComponentType,
  parameters: { withRouter: false },
};
