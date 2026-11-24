import { useEffect, useState } from "react";

import { formatAge, observationState, observationSummary } from "./controllerObservation";
import { Panel, Stat } from "./diagnosticLayout";
import { useControllerDiagnostics } from "./useControllerDiagnostics";
import type {
  ControllerDiagnostics as Controller,
  ControllerDiagnosticsResult,
  ControllerObservation,
  ControllerRecovery,
  ControllerRole,
  HaIdentityState,
} from "@/protoOS/api/adapters/controllerDiagnostics";
import StatusCircle from "@/shared/components/StatusCircle";
import type { StatusCircleStatus } from "@/shared/components/StatusCircle/constants";

const NOT_REPORTED = "Not reported";

const roleLabels: Record<ControllerRole, string> = {
  primary: "Primary",
  standby: "Standby",
  fenced: "Fenced",
  unknown: "Unknown",
};

const roleHealth: Record<ControllerRole, StatusCircleStatus> = {
  primary: "normal",
  standby: "normal",
  fenced: "error",
  unknown: "inactive",
};

const identityLabels: Record<HaIdentityState, string> = {
  confirmed: "Confirmed",
  conflicted: "Conflicted",
  unavailable: "Unavailable",
  unknown: "Unknown",
};

const recoveryLabels: Record<ControllerRecovery["state"], string> = {
  steady: "Steady",
  reconciling: "Reconciling",
  degraded: "Degraded",
  unknown: "Unknown",
};

const phaseLabels: Record<ControllerRecovery["phase"], string> = {
  steady: "Steady",
  database: "Database",
  controlState: "Control state",
  assignments: "Board assignments",
  flotillaSync: "Flotilla sync",
  unknown: "Unknown phase",
};

const reported = (value: boolean | undefined, yes: string, no: string) =>
  value === undefined ? NOT_REPORTED : value ? yes : no;

const recoverySummary = (recovery: ControllerRecovery | undefined) => {
  if (!recovery) return NOT_REPORTED;
  if (recovery.state === "steady") return recoveryLabels.steady;
  const parts = [recoveryLabels[recovery.state]];
  if (recovery.phase !== "steady") parts.push(phaseLabels[recovery.phase]);
  if (recovery.expectedBoards !== undefined) {
    parts.push(`${recovery.reconciledBoards ?? 0} of ${recovery.expectedBoards} boards`);
  }
  return parts.join(" · ");
};

// Keeps observation ages current between polls.
const useNow = () => {
  const [nowMs, setNowMs] = useState(Date.now);
  useEffect(() => {
    const id = setInterval(() => setNowMs(Date.now()), 1_000);
    return () => clearInterval(id);
  }, []);
  return nowMs;
};

/** A last-reported value qualified by its own observation age. */
const ObservedValue = ({
  value,
  health,
  observation,
  nowMs,
  testId,
}: {
  value: string;
  health: StatusCircleStatus;
  observation: ControllerObservation | undefined;
  nowMs: number;
  testId: string;
}) => {
  const state = observationState(observation, nowMs);
  return (
    <>
      <span className="flex items-center gap-2">
        <StatusCircle
          status={state === "fresh" ? health : state === "stale" ? "warning" : "inactive"}
          variant="simple"
          width="w-2"
          removeMargin
          testId={testId}
        />
        {value}
      </span>
      <span className="mt-1 block text-200 text-text-primary-70" data-observation={state}>
        {observationSummary(observation, state, nowMs)}
      </span>
      {observation?.detail ? (
        <span className="mt-1 block text-200 text-text-primary-70">{observation.detail}</span>
      ) : null}
    </>
  );
};

const missingNote = (result: ControllerDiagnosticsResult, controller: Controller) => {
  if (controller.reported) return undefined;
  if (controller.local) {
    return result.status.status === "unavailable" ? `Status unavailable: ${result.status.message}` : undefined;
  }
  switch (result.ha.status) {
    case "unsupported":
      return "This controller does not report HA status, so its peer cannot be shown.";
    case "unavailable":
      return `HA status unavailable: ${result.ha.message}`;
    case "reported":
      return "The HA report names this peer but includes no observations of it.";
  }
};

const ControllerCard = ({
  controller,
  result,
  nowMs,
}: {
  controller: Controller;
  result: ControllerDiagnosticsResult;
  nowMs: number;
}) => {
  const placement = controller.local ? "This controller" : "Peer controller";
  const note = missingNote(result, controller);
  // Without an enabled HA report the role is the live SystemStatus value, which has no observation to age.
  const observed = result.ha.status === "reported" && result.ha.enabled;
  const role = roleLabels[controller.role];
  const writes = reported(controller.writePrimary, "Authorized", "Not authorized");
  const reachability = reported(controller.reachable, "Reachable", "Unreachable");
  const reachabilityHealth =
    controller.reachable === undefined ? "inactive" : controller.reachable ? "normal" : "error";

  return (
    <Panel>
      <div className="mb-6">
        <h3 className="text-emphasis-300 break-words">{controller.hostname ?? controller.id ?? placement}</h3>
        <p className="text-300 text-text-primary-70">{placement}</p>
        {note ? <p className="mt-2 text-300 text-text-primary-70">{note}</p> : null}
      </div>
      <dl className="grid grid-cols-1 gap-8 tablet:grid-cols-2">
        <Stat label="Server ID" value={<span className="break-words">{controller.id ?? NOT_REPORTED}</span>} />
        <Stat label="Version" value={controller.version ?? NOT_REPORTED} />
        <Stat
          label="HA role"
          value={
            observed ? (
              <ObservedValue
                value={role}
                health={roleHealth[controller.role]}
                observation={controller.roleObservation}
                nowMs={nowMs}
                testId="controller-role-status"
              />
            ) : (
              role
            )
          }
        />
        <Stat
          label="Container writes"
          value={
            observed ? (
              // Write authority is reported with the role, so it ages with the role observation.
              <ObservedValue
                value={writes}
                health={controller.writePrimary === undefined ? "inactive" : "normal"}
                observation={controller.roleObservation}
                nowMs={nowMs}
                testId="controller-writes-status"
              />
            ) : (
              writes
            )
          }
        />
        <Stat
          label="Reachability"
          value={
            observed ? (
              <ObservedValue
                value={reachability}
                health={reachabilityHealth}
                observation={controller.reachabilityObservation}
                nowMs={nowMs}
                testId="controller-reachability-status"
              />
            ) : (
              reachability
            )
          }
        />
        <Stat label="Recovery" value={recoverySummary(controller.recovery)} />
      </dl>
      {controller.recovery?.detail ? (
        <p className="mt-6 text-300 text-text-primary-70">{controller.recovery.detail}</p>
      ) : null}
    </Panel>
  );
};

const ClusterSummary = ({ result, nowMs }: { result: ControllerDiagnosticsResult; nowMs: number }) => {
  const { ha, status } = result;
  const haReported = ha.status === "reported" ? ha : undefined;
  const haState =
    ha.status === "reported"
      ? ha.enabled
        ? "Enabled"
        : "Not enabled"
      : ha.status === "unsupported"
        ? "Not supported"
        : "Unavailable";
  return (
    <Panel>
      <dl className="grid grid-cols-2 gap-8 tablet:grid-cols-3 desktop:grid-cols-6">
        <Stat label="High availability" value={haState} />
        <Stat
          label="Cluster identity"
          value={haReported?.enabled ? identityLabels[haReported.identityState] : NOT_REPORTED}
        />
        <Stat
          label="Identity inventory"
          value={haReported?.enabled ? (haReported.inventoryReady ? "Loaded" : "Not loaded") : NOT_REPORTED}
        />
        <Stat
          label="HA report"
          value={haReported?.observedAtMs === undefined ? NOT_REPORTED : formatAge(haReported.observedAtMs, nowMs)}
        />
        <Stat
          label="Database"
          value={
            status.status === "reported" ? (status.databaseConnected ? "Connected" : "Disconnected") : NOT_REPORTED
          }
        />
        <Stat
          label="Config store"
          value={reported(status.status === "reported" ? status.dcsHealthy : undefined, "Healthy", "Unhealthy")}
        />
      </dl>
      {haReported?.clusterId ? (
        <p className="mt-6 text-300 break-words text-text-primary-70">Cluster {haReported.clusterId}</p>
      ) : null}
      {ha.status === "unavailable" ? (
        <p className="mt-2 text-300 text-text-primary-70">HA status unavailable: {ha.message}</p>
      ) : null}
      {haReported?.detail ? <p className="mt-2 text-300 text-text-primary-70">{haReported.detail}</p> : null}
      {haReported?.conflicts.length ? (
        <ul role="alert" className="mt-2 list-disc pl-5 text-300 text-intent-critical-fill">
          {haReported.conflicts.map((conflict) => (
            <li key={conflict}>{conflict}</li>
          ))}
        </ul>
      ) : null}
    </Panel>
  );
};

const ControllerDiagnostics = () => {
  const { supported, result, error } = useControllerDiagnostics();
  const nowMs = useNow();
  if (!supported) return null;

  return (
    <section aria-labelledby="controller-diagnostics-title">
      <h2 id="controller-diagnostics-title" className="mb-6 text-heading-200">
        Controllers
      </h2>
      {error ? (
        <p role="alert" className="mb-4 text-300 text-text-critical">
          {error}
        </p>
      ) : null}
      {result ? (
        <div className="space-y-1">
          <ClusterSummary result={result} nowMs={nowMs} />
          <div className="grid gap-1 tablet:grid-cols-2">
            {result.controllers.map((controller, index) => (
              <ControllerCard
                key={controller.id ?? `${controller.local ? "local" : "peer"}-${index}`}
                controller={controller}
                result={result}
                nowMs={nowMs}
              />
            ))}
          </div>
        </div>
      ) : error ? null : (
        <p role="status">Loading controllers…</p>
      )}
    </section>
  );
};

export default ControllerDiagnostics;
