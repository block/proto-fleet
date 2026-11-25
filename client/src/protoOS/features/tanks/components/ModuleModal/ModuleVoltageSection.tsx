import { useEffect, useRef, useState } from "react";
import {
  canSetFixed,
  formatRange,
  modeLabel,
  parseMillivolts,
  readinessNote,
  type VoltageOutcome,
  voltageOutcome,
  type VoltageRequest,
} from "./moduleVoltageView";
import { ct1ReportedModuleId } from "@/protoOS/api/adapters/mdkV2/normalize";
import {
  findModuleVoltageSetting,
  MODULE_VOLTAGE_OUTCOME_UNKNOWN,
  type ModuleVoltageSetting,
  type ModuleVoltageWriteResult,
} from "@/protoOS/api/adapters/moduleVoltage";
import type { AdapterRequestOptions } from "@/protoOS/api/adapters/types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { OUTCOME_TONES } from "@/protoOS/features/containerHome/hooks/usePlantAlarmOutcome";
import ComponentSection from "@/protoOS/features/diagnostic/components/ComponentSection";
import type { ModuleNode } from "@/protoOS/store/contract";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import useMinerStore from "@/protoOS/store/useMinerStore";
import Button, { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import Input from "@/shared/components/Input";
import { pushToast } from "@/shared/features/toaster";
import { formatTimestamp } from "@/shared/utils/formatTimestamp";
import { withDeadline } from "@/shared/utils/withDeadline";

// The controller saves the setting, then applies it to each member board before it answers.
export const VOLTAGE_WRITE_TIMEOUT_MS = 60_000;
const READ_TIMEOUT_MS = 15_000;

type Load =
  | { state: "loading" }
  | { state: "failed"; message: string }
  /** setting is null when the controller does not list this module. */
  | { state: "loaded"; setting: ModuleVoltageSetting | null };

type Editing = { kind: "fixed"; value: string; touched: boolean } | { kind: "automatic" };

const errorMessage = (error: unknown) =>
  error instanceof Error
    ? error.message
    : ((error as { error?: { message?: string } })?.error?.message ?? "The request did not complete.");

const writeFailure = (error: unknown) => {
  const failed = error as { message?: string; writeOutcome?: "refused" | "unknown"; status?: number } | undefined;
  if (failed?.writeOutcome === "refused" && failed.message)
    return { message: failed.message, writeOutcome: "refused" as const };
  // Authentication failed before the controller processed anything.
  if (failed?.status === 401) {
    return {
      message: "Sign in again before trying again. The voltage setting was not changed.",
      writeOutcome: "refused" as const,
    };
  }
  return { message: MODULE_VOLTAGE_OUTCOME_UNKNOWN, writeOutcome: "unknown" as const };
};

/**
 * Module-wide fixed voltage or automatic tuning. Reads the saved setting before offering a change, sends
 * each change once, and confirms it with a separate read.
 */
export default function ModuleVoltageSection({
  module,
  onWritingChange,
}: {
  module: ModuleNode;
  /** Reports whether a write and its re-read are in flight, so the host can keep this module on screen. */
  onWritingChange?: (writing: boolean) => void;
}) {
  const { adapter, isFleetHosted } = useMinerHosting();
  const authRetry = useAuthRetry();
  const wireId = ct1ReportedModuleId(module.id);
  const [load, setLoad] = useState<Load>({ state: "loading" });
  const [editing, setEditing] = useState<Editing>();
  /** The mode being written, while a write and its re-read are in flight. */
  const [writing, setWriting] = useState<"fixed" | "automatic">();
  const [outcome, setOutcome] = useState<VoltageOutcome>();
  const isWriting = Boolean(writing);
  useEffect(() => {
    onWritingChange?.(isWriting);
  }, [isWriting, onWritingChange]);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const hashboards = useMinerStore((state) => state.hardware.topology?.hashboards);
  const boardLabel = (mac: string) => {
    const board = module.hashboardIds
      .map((id) => hashboards?.get(id))
      .find((item) => item?.identity.kind === "mac" && item.identity.value.toLowerCase() === mac.toLowerCase());
    return board?.location?.slot ? `Board ${board.location.slot}` : mac;
  };

  const supported = Boolean(
    adapter?.listModuleVoltageSettings && adapter.setModuleFixedVoltage && adapter.clearModuleFixedVoltage,
  );

  /** Resolves to the module's setting, null when it is not listed, or throws. */
  const read = async (isCurrent: () => boolean) => {
    const controller = new AbortController();
    let settings: ModuleVoltageSetting[] | undefined;
    let failure: unknown;
    await withDeadline(
      () =>
        authRetry({
          signal: controller.signal,
          shouldRetry: isCurrent,
          request: (params: AdapterRequestOptions) => adapter!.listModuleVoltageSettings!(params),
          onSuccess: (value) => {
            settings = value;
          },
          onError: (error) => {
            failure = error;
          },
        }),
      controller,
      READ_TIMEOUT_MS,
    );
    if (failure || !settings) throw failure ?? new Error("The controller did not return voltage settings.");
    return findModuleVoltageSetting(settings, wireId!) ?? null;
  };

  // Sets state only once the read settles.
  const fetchSetting = () => {
    const generation = useMinerStore.getState().connectionGeneration;
    const isCurrent = () => mounted.current && useMinerStore.getState().connectionGeneration === generation;
    read(isCurrent).then(
      (setting) => {
        if (isCurrent()) setLoad({ state: "loaded", setting });
      },
      (error) => {
        if (isCurrent())
          setLoad({ state: "failed", message: `Could not load the voltage setting: ${errorMessage(error)}` });
      },
    );
  };
  const reload = () => {
    // A fresh read supersedes an outcome that was waiting on one.
    setOutcome(undefined);
    setLoad({ state: "loading" });
    fetchSetting();
  };

  // Load once per controller connection and module, not whenever a callback changes identity.
  const fetchLatest = useRef(fetchSetting);
  useEffect(() => {
    fetchLatest.current = fetchSetting;
  });
  useEffect(() => {
    if (supported && !isFleetHosted && wireId) fetchLatest.current();
  }, [adapter, supported, isFleetHosted, wireId]);

  // Fleet-hosted sessions cannot reach these controller endpoints yet.
  if (!supported || isFleetHosted) return null;

  const send = async (request: VoltageRequest) => {
    if (!wireId) return;
    const generation = useMinerStore.getState().connectionGeneration;
    const isCurrent = () => mounted.current && useMinerStore.getState().connectionGeneration === generation;
    const previous = load.state === "loaded" ? load.setting : undefined;
    setWriting(request === undefined ? "automatic" : "fixed");
    setOutcome(undefined);
    const controller = new AbortController();
    let result: ModuleVoltageWriteResult | undefined;
    let failed: unknown;
    try {
      // One command. Only an authentication rejection, which the controller never processed, is re-sent after
      // the token refresh; a timeout or any other failure leaves the outcome to the re-read below.
      await withDeadline(
        () =>
          authRetry({
            signal: controller.signal,
            shouldRetry: () => isCurrent() && !controller.signal.aborted,
            request: (params: AdapterRequestOptions) =>
              request === undefined
                ? adapter!.clearModuleFixedVoltage!(module.id, params)
                : adapter!.setModuleFixedVoltage!(module.id, request, params),
            onSuccess: (value) => {
              result = value;
            },
            onError: (error) => {
              failed = error;
            },
          }),
        controller,
        VOLTAGE_WRITE_TIMEOUT_MS,
      );
    } catch (error) {
      failed = error;
    }
    if (!isCurrent()) {
      if (mounted.current) setWriting(undefined);
      return;
    }
    let reread: ModuleVoltageSetting | null | undefined;
    try {
      reread = await read(isCurrent);
    } catch {
      reread = undefined;
    }
    if (!isCurrent()) {
      if (mounted.current) setWriting(undefined);
      return;
    }
    setLoad(
      reread === undefined
        ? { state: "failed", message: "Could not re-read the voltage setting." }
        : { state: "loaded", setting: reread },
    );
    const settled = voltageOutcome({
      request,
      write: result && !failed ? { result } : { error: writeFailure(failed) },
      previous,
      reread,
      boardLabel,
    });
    // A confirmed change is already shown by the mode above, so it only needs a passing notice. Anything the
    // operator may have to act on stays in the section.
    if (settled.tone === "neutral" && !settled.boards?.length) {
      pushToast({ message: settled.message, status: "success", ttl: 3000 });
    } else {
      setOutcome(settled);
    }
    setWriting(undefined);
  };

  const setting = load.state === "loaded" ? load.setting : undefined;
  const range = setting?.supportedRange;
  const parsed = editing?.kind === "fixed" && range ? parseMillivolts(editing.value, range) : undefined;
  const note = setting ? readinessNote(setting) : undefined;
  const confirmFixed = () => {
    if (!parsed || "error" in parsed) return;
    setEditing(undefined);
    void send(parsed.millivolts);
  };

  return (
    <ComponentSection title="Voltage">
      <div className="space-y-4" aria-busy={load.state === "loading" || Boolean(writing)}>
        {!wireId ? (
          <p role="status" className="text-200 text-text-primary-70">
            The controller has not reported an identity for this module, so its voltage cannot be changed.
          </p>
        ) : load.state === "loading" && !writing ? (
          <p role="status" className="text-200 text-text-primary-70">
            Loading voltage setting…
          </p>
        ) : load.state === "failed" ? (
          <div className="flex items-center gap-4">
            <p role="alert" className="text-200 text-text-critical">
              {load.message}
            </p>
            <Button variant="secondary" size="compact" text="Reload" disabled={Boolean(writing)} onClick={reload} />
          </div>
        ) : setting === null ? (
          <p role="status" className="text-200 text-text-primary-70">
            The controller does not list this module, so its voltage cannot be changed. It may have been replaced.
          </p>
        ) : setting ? (
          <>
            <dl className="grid grid-cols-1 gap-4 tablet:grid-cols-3">
              <div>
                <dt className="text-200 text-text-primary-70">Mode</dt>
                <dd className="text-emphasis-300">{modeLabel(setting)}</dd>
              </div>
              <div>
                <dt className="text-200 text-text-primary-70">Supported range</dt>
                <dd className="text-emphasis-300">{range ? formatRange(range) : "Unavailable"}</dd>
              </div>
              <div>
                <dt className="text-200 text-text-primary-70">Last changed</dt>
                <dd className="text-emphasis-300">
                  {setting.updatedAt
                    ? [
                        formatTimestamp(setting.updatedAt.getTime() / 1000),
                        setting.updatedBy && `by ${setting.updatedBy}`,
                      ]
                        .filter(Boolean)
                        .join(" ")
                    : "Not reported"}
                </dd>
              </div>
            </dl>
            {note ? <p className="text-200 text-text-primary-70">{note}</p> : null}
            <div className="flex flex-wrap gap-2">
              <Button
                variant="secondary"
                size="compact"
                text={setting.fixedMillivolts === undefined ? "Set fixed voltage" : "Change fixed voltage"}
                loading={writing === "fixed"}
                disabled={Boolean(writing) || !canSetFixed(setting)}
                onClick={() =>
                  setEditing({
                    kind: "fixed",
                    value: setting.fixedMillivolts === undefined ? "" : String(setting.fixedMillivolts),
                    touched: false,
                  })
                }
              />
              {setting.fixedMillivolts !== undefined ? (
                <Button
                  variant="secondary"
                  size="compact"
                  text="Use automatic voltage"
                  loading={writing === "automatic"}
                  disabled={Boolean(writing)}
                  onClick={() => setEditing({ kind: "automatic" })}
                />
              ) : null}
            </div>
          </>
        ) : null}
        {outcome ? (
          <div className="flex items-start justify-between gap-4">
            <div
              role={outcome.tone === "neutral" ? "status" : "alert"}
              className={`text-200 ${OUTCOME_TONES[outcome.tone]}`}
            >
              <p>{outcome.message}</p>
              {outcome.boards?.length ? (
                <ul className="mt-1 list-disc pl-5">
                  {outcome.boards.map((board) => (
                    <li key={board.label}>
                      {board.label}: {board.message}
                    </li>
                  ))}
                </ul>
              ) : null}
            </div>
            <div className="flex shrink-0 gap-2">
              {/* Anything short of a confirmed change may still settle, so offer another read in place. */}
              {outcome.tone !== "neutral" && load.state === "loaded" ? (
                <Button variant="textOnly" text="Check again" disabled={isWriting} onClick={reload} />
              ) : null}
              <Button variant="textOnly" text="Dismiss" onClick={() => setOutcome(undefined)} />
            </div>
          </div>
        ) : null}
      </div>
      {editing?.kind === "fixed" && range ? (
        <Dialog
          title="Set a fixed module voltage?"
          subtitle="Applies to every hashboard in this module and stops automatic voltage tuning for them until you switch back."
          manageFocus
          preventScroll
          onDismiss={() => setEditing(undefined)}
          buttons={[
            { text: "Cancel", variant: variants.secondary, onClick: () => setEditing(undefined) },
            {
              text: "Set voltage",
              variant: variants.primary,
              disabled: !parsed || "error" in parsed,
              onClick: confirmFixed,
            },
          ]}
        >
          <Input
            id="module-fixed-voltage"
            label="Fixed voltage"
            units="mV"
            inputMode="numeric"
            autoFocus
            initValue={editing.value}
            onChange={(value) => setEditing({ kind: "fixed", value, touched: true })}
            onKeyDown={(key) => {
              if (key === "Enter") confirmFixed();
            }}
            error={editing.touched && parsed && "error" in parsed ? parsed.error : undefined}
          />
          <p className="mt-2 text-200 text-text-primary-70">Supported range: {formatRange(range)}.</p>
        </Dialog>
      ) : null}
      {editing?.kind === "automatic" ? (
        <Dialog
          title="Return to automatic voltage?"
          subtitle="Firmware tunes every hashboard in this module again."
          manageFocus
          preventScroll
          onDismiss={() => setEditing(undefined)}
          buttons={[
            { text: "Cancel", variant: variants.secondary, onClick: () => setEditing(undefined) },
            {
              text: "Use automatic voltage",
              variant: variants.primary,
              onClick: () => {
                setEditing(undefined);
                void send(undefined);
              },
            },
          ]}
        />
      ) : null}
    </ComponentSection>
  );
}
