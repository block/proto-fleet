import type { ReactNode } from "react";
import { create } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { isCurrentObservation } from "./moduleDetail";
import { ModuleIssues } from "./ModuleIssues";
import { ModuleStatus } from "./ModuleStatus";
import { getModuleStatusPresentation } from "./moduleStatusPresentation";
import {
  type HashboardReading,
  ModuleIssuesSchema,
} from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import type { HashboardSlot } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { useTemperatureUnit } from "@/protoFleet/store";
import ComponentMetadata from "@/shared/components/StatusModal/ComponentMetadata";
import { formatEfficiencyOrDash, formatHashrateOrDash, formatPowerKwOrDash } from "@/shared/utils/telemetryFormat";

const unavailableReasons: Record<string, string> = {
  invalid_observation: "Invalid board data",
  ambiguous_slot: "Slot position conflict",
  missing_identity: "Board identity missing",
  identity_conflict: "Board identity conflict",
  controller_conflict: "Board reported by another controller",
  older_observation: "Newer board observation exists",
};

// CT1 folds shared PSU thermal issues into each board's aggregate attention state.
// Only suppress the unknown-fault fallback when no board-local cause is reported.
function hasOnlyPsuAttention(board: HashboardReading, now: number | undefined) {
  const health = board.health;
  if (now === undefined || !health || !isCurrentObservation(board.observedAt, now)) return false;
  if (
    !Array.isArray(health.errors) ||
    health.errors.length ||
    !Array.isArray(health.warnings) ||
    health.warnings.length
  )
    return false;
  if (board.miningState === "MINING_STATE_ERROR") return false;
  if (board.miningState === "MINING_STATE_MINING" && (board.hashrailOk === false || board.refclksOk === false))
    return false;
  const thermal = health.psuThermal;
  if (!thermal || typeof thermal !== "object" || Array.isArray(thermal) || thermal.thermalStateValid !== true)
    return false;
  if (thermal.overTemperatureWarning !== true && thermal.overTemperatureFault !== true) return false;
  // An omitted age is permitted by the CT1 collector; otherwise include the
  // embedded sample age so a fresh board response cannot revive old PSU data.
  const age = thermal.thermalStateAgeMs ?? 0;
  if (typeof age !== "number" || !Number.isFinite(age) || age < 0) return false;
  const sourceMs = Number(board.observedAt!.seconds) * 1000 + board.observedAt!.nanos / 1e6 - age;
  return isCurrentObservation(timestampFromMs(sourceMs), now);
}

export function HashboardIssues({
  board,
  position,
  name,
  iconOnly = false,
  renderTrigger,
  now,
}: {
  board: HashboardReading;
  position: number;
  name: string;
  iconOnly?: boolean;
  now?: number;
  renderTrigger?: (open: () => void, hasIssues: boolean) => ReactNode;
}) {
  const items = (["errors", "warnings"] as const).flatMap((kind) => {
    const values = board.health?.[kind];
    if (!Array.isArray(values)) return [];
    return values.flatMap((issue) => {
      if (!issue || typeof issue !== "object" || !("cause" in issue) || typeof issue.cause !== "string") return [];
      const stack =
        "stack" in issue && (typeof issue.stack === "number" || typeof issue.stack === "string")
          ? ` (stack ${issue.stack})`
          : "";
      return [
        {
          component: "hashboard",
          componentId: board.macAddress,
          cause: `${issue.cause}${stack}`,
          severity: kind === "errors" ? "fault" : "warning",
          observedAt: board.observedAt,
        },
      ];
    });
  });
  if ((iconOnly || renderTrigger) && !items.length && board.state === "attention" && !hasOnlyPsuAttention(board, now)) {
    items.push({
      component: "hashboard",
      componentId: board.macAddress,
      cause: "Fault reported; details unavailable",
      severity: "fault",
      observedAt: board.stateObservedAt,
    });
  }
  const evidence = create(ModuleIssuesSchema, {
    observedAt: board.observedAt,
    complete: board.health !== undefined,
    items,
  });
  const status = getModuleStatusPresentation(board.state);
  return (
    <ModuleIssues
      item={{ name, moduleIssues: evidence }}
      hashboardPosition={position}
      iconOnly={iconOnly}
      renderTrigger={renderTrigger}
      status={renderTrigger ? (status.label === "Unknown" ? "Status unknown" : status.label) : undefined}
    >
      <ComponentMetadata
        metadata={{
          serial: { label: "Serial number", value: board.serial || undefined },
          mac: { label: "MAC address", value: board.macAddress || undefined },
          firmware: {
            label: "Firmware",
            value: typeof board.metadata?.firmwareVersion === "string" ? board.metadata.firmwareVersion : undefined,
          },
        }}
      />
    </ModuleIssues>
  );
}

export function HashboardCell({ slot, column }: { slot: HashboardSlot; column: string }) {
  const unit = useTemperatureUnit();
  const board = slot.occupancy === "occupied" ? slot.board : undefined;
  const identity = board?.serial || board?.macAddress;
  const name = `Hashboard ${slot.position}${identity ? ` - ${identity}` : ""}`;
  switch (column) {
    case "name":
      return (
        <span className="block truncate pl-8" title={name}>
          {name}
        </span>
      );
    case "status":
      if (slot.occupancy === "empty") return <span>Empty slot</span>;
      if (!board)
        return (
          <span className="whitespace-normal">
            Unavailable
            {slot.unavailableReason ? (
              <span className="block text-xs text-text-primary-50">
                {unavailableReasons[slot.unavailableReason] ?? "Board data rejected"}
              </span>
            ) : null}
          </span>
        );
      return <ModuleStatus state={board.state} />;
    case "hashrate":
      return <span>{formatHashrateOrDash(board?.hashrateHs === undefined ? null : board.hashrateHs / 1e12)}</span>;
    case "power":
    case "powerUsage":
      return <span>{formatPowerKwOrDash(board?.powerW === undefined ? null : board.powerW / 1000)}</span>;
    case "efficiency":
      return (
        <span>{formatEfficiencyOrDash(board?.efficiencyJh === undefined ? null : board.efficiencyJh * 1e12)}</span>
      );
    case "temperature": {
      const celsius = board?.temperatureC;
      return (
        <span>
          {celsius === undefined ? "—" : `${Math.round(unit === "F" ? celsius * 1.8 + 32 : celsius)} °${unit}`}
        </span>
      );
    }
    case "macAddress":
      return <span>{board?.macAddress || "—"}</span>;
    case "issues":
      return board ? <HashboardIssues board={board} position={slot.position} name={name} /> : null;
    default:
      return <span>—</span>;
  }
}
