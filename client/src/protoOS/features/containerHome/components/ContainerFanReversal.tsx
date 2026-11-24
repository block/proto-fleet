import { useEffect, useRef, useState } from "react";
import {
  dismissFanReversalOutcome,
  fanReversalMessage,
  fanReversalWaitingMessage,
  isFanReversalActive,
  recordFanReversalReadback,
  setFanReversalOutcome,
  settleFanReversal,
  useFanReversalOutcome,
} from "../hooks/useFanReversalOutcome";
import { readFanReversalSettings, toFanReversalReadback, useDryCoolerHold } from "../hooks/useFanReversalReadback";
import { OUTCOME_TONES } from "../hooks/usePlantAlarmOutcome";
import {
  DEFAULT_FAN_REVERSAL_MINUTES,
  FAN_REVERSAL_DURATIONS,
  FAN_REVERSAL_STATE_LABELS,
  fanReversalAvailability,
  fanReversalDeadlineMs,
  fanReversalOutcome,
} from "../utils/fanReversal";
import type { AdapterRequestOptions, ContainerSettings, MinerAdapter } from "@/protoOS/api/adapters/types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import useMinerStore from "@/protoOS/store/useMinerStore";
import Button, { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import Select from "@/shared/components/Select";
import { withDeadline } from "@/shared/utils/withDeadline";

const dryCoolerMode = (settings?: Pick<ContainerSettings, "controls">) =>
  settings?.controls.find((control) => control.name === "dry_cooler_auto")?.enabled;

interface ContainerFanReversalProps {
  settings: ContainerSettings;
  /** When `settings` was requested from the controller. */
  settingsRequestedAtMs: number;
  /** True while a System controls change is still being saved; a run waits for it. */
  settingsPending?: boolean;
  /** Called once when a run settles, with the controller read taken after it (unset if it failed). */
  onSettled?: (current: ContainerSettings | undefined) => void;
  /** The dry cooler mode the System controls show now (`dry_cooler_auto`), unset when unknown. */
  dryCoolerAuto?: boolean;
  /** True while the confirmation is open, so the host can keep itself open. */
  onBusyChange?: (busy: boolean) => void;
}

export default function ContainerFanReversal({
  settings,
  settingsRequestedAtMs,
  settingsPending,
  dryCoolerAuto,
  onSettled,
  onBusyChange,
}: ContainerFanReversalProps) {
  const { adapter, isFleetHosted } = useMinerHosting();
  const authRetry = useAuthRetry();
  const { generation, outcome, readback, postRun } = useFanReversalOutcome();
  const fanCount = useMinerStore(
    (state) => [...state.hardware.fans.values()].filter((fan) => fan.ref.owner.kind === "container").length,
  );
  const [confirming, setConfirming] = useState<{ durationMinutes: number; generation: number }>();
  useEffect(() => onBusyChange?.(confirming !== undefined), [confirming, onBusyChange]);

  // The settings this modal loaded are a reading too; keep whichever is newer.
  useEffect(() => {
    recordFanReversalReadback(
      useMinerStore.getState().connectionGeneration,
      toFanReversalReadback(settings, settingsRequestedAtMs),
    );
  }, [settings, settingsRequestedAtMs]);

  // Hand the read taken after a run to the host, whichever instance of this section started it;
  // a read from before this one mounted is older than the settings it loaded.
  const handledPostRun = useRef(postRun);
  useEffect(() => {
    if (!postRun || postRun === handledPostRun.current) return;
    handledPostRun.current = postRun;
    onSettled?.(postRun.settings);
  }, [postRun, onSettled]);

  const status = readback?.status;
  const active = isFanReversalActive(status);
  const running = outcome?.phase === "running";
  // Follow the procedure while it holds the cooler, and until the controller reports a settled
  // state: without one the section says it is waiting, so it has to keep reading.
  useDryCoolerHold(!status || status.state === "unknown");

  const retained = status?.durationMinutes;
  const [duration, setDuration] = useState<number>();
  const selectedDuration =
    duration ??
    (retained !== undefined && FAN_REVERSAL_DURATIONS.includes(retained) ? retained : DEFAULT_FAN_REVERSAL_MINUTES);

  const send = async (durationMinutes: number, sendGeneration: number) => {
    if (useMinerStore.getState().connectionGeneration !== sendGeneration) return;
    if (!adapter?.runDryCoolerFanReversal) return;
    const isCurrent = () => useMinerStore.getState().connectionGeneration === sendGeneration;
    const modeBefore = dryCoolerAuto;
    setFanReversalOutcome(sendGeneration, { phase: "running", durationMinutes });
    const controller = new AbortController();
    let result: Awaited<ReturnType<NonNullable<MinerAdapter["runDryCoolerFanReversal"]>>> | undefined;
    let failed: unknown;
    try {
      // One command. Only an authentication rejection, which the controller never processed, is
      // re-sent after the token refresh. It is never aborted when the modal closes.
      await withDeadline(
        () =>
          authRetry({
            signal: controller.signal,
            shouldRetry: () => isCurrent() && !controller.signal.aborted,
            request: (params: AdapterRequestOptions) =>
              adapter.runDryCoolerFanReversal!.call(adapter, durationMinutes, params),
            onSuccess: (value) => {
              result = value;
            },
            onError: (error) => {
              failed = error;
            },
          }),
        controller,
        fanReversalDeadlineMs(durationMinutes),
      );
    } catch (error) {
      failed = error;
    }
    if (!isCurrent()) return;
    // A separate read, so a failed read can never replay the command.
    let current: ContainerSettings | undefined;
    try {
      const requestedAtMs = Date.now();
      current = adapter.getContainerSettings ? await readFanReversalSettings(adapter, authRetry, isCurrent) : undefined;
      if (!isCurrent()) return;
      recordFanReversalReadback(sendGeneration, toFanReversalReadback(current, requestedAtMs));
    } catch {
      // The outcome says the mode could not be re-read, and the host re-reads before its next change.
    }
    if (!isCurrent()) return;
    settleFanReversal(
      sendGeneration,
      fanReversalOutcome({
        durationMinutes,
        result,
        error: failed,
        modeBefore,
        modeAfter: current ? dryCoolerMode(current) : null,
        nowMs: Date.now(),
      }),
      current,
    );
  };

  const availability = fanReversalAvailability({ adapter, isFleetHosted, outcome, readback, settingsPending });
  const result = fanReversalMessage(outcome);
  // The retained duration is the picker's default; only a run in progress needs it beside the state.
  const stateLine = status
    ? [FAN_REVERSAL_STATE_LABELS[status.state], active && status.durationMinutes && `${status.durationMinutes} min`]
        .filter(Boolean)
        .join(" · ")
    : "Not reported";
  const scope = fanCount > 0 ? `all ${fanCount} dry cooler fans` : "every dry cooler fan";

  return (
    <section aria-label="Fan reversal" className="space-y-4">
      <div>
        <h2 className="text-heading-100">Fan reversal</h2>
        <p className="text-200 text-text-primary-70">Runs {scope} in reverse to clear debris.</p>
      </div>
      <dl className="grid grid-cols-3 gap-4">
        <div>
          <dt className="text-200 text-text-primary-70">Controller state</dt>
          <dd className="text-emphasis-300" data-testid="fan-reversal-state">
            {stateLine}
          </dd>
        </div>
      </dl>
      <div className="grid grid-cols-1 gap-6 tablet:grid-cols-2">
        <Select
          id="system-fan-reversal-duration"
          label="Duration"
          options={FAN_REVERSAL_DURATIONS.map((minutes) => ({ value: String(minutes), label: `${minutes} min` }))}
          value={String(selectedDuration)}
          disabled={!availability.available}
          onChange={(value) => setDuration(Number(value))}
        />
      </div>
      <div className="space-y-2">
        <Button
          variant="secondary"
          size="compact"
          text="Run fan reversal"
          loading={running}
          disabled={!availability.available}
          onClick={() =>
            setConfirming({
              durationMinutes: selectedDuration,
              generation: useMinerStore.getState().connectionGeneration,
            })
          }
        />
        {!availability.available && availability.reason ? (
          <p className="text-200 text-text-primary-70">{availability.reason}</p>
        ) : null}
      </div>
      {outcome?.phase === "running" ? (
        <p role="status" className="text-200 text-text-primary-70">
          {fanReversalWaitingMessage(outcome.durationMinutes)}
        </p>
      ) : null}
      {result ? (
        <div className="flex items-start justify-between gap-4">
          <p role={result.tone === "neutral" ? "status" : "alert"} className={`text-200 ${OUTCOME_TONES[result.tone]}`}>
            {result.message}
          </p>
          <Button variant="textOnly" text="Dismiss" onClick={() => dismissFanReversalOutcome(generation)} />
        </div>
      ) : null}
      {confirming ? (
        <Dialog
          title={`Run fan reversal for ${confirming.durationMinutes} minutes?`}
          subtitle={`Affects ${scope}: they run in reverse for ${confirming.durationMinutes} minutes, then the controller ends the procedure. ProtoOS cannot stop it early and does not change the dry cooler's power, Auto or Manual mode, or speed.`}
          manageFocus
          preventScroll
          onDismiss={() => setConfirming(undefined)}
          buttons={[
            { text: "Cancel", variant: variants.secondary, onClick: () => setConfirming(undefined) },
            {
              text: "Run fan reversal",
              variant: variants.primary,
              disabled: !availability.available,
              onClick: () => {
                const { durationMinutes, generation: confirmedGeneration } = confirming;
                setConfirming(undefined);
                if (!availability.available) return;
                void send(durationMinutes, confirmedGeneration);
              },
            },
          ]}
        />
      ) : null}
    </section>
  );
}
