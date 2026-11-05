import type { ReactNode } from "react";
import { create } from "@bufbuild/protobuf";
import HashboardPoolAction from "./HashboardPoolAction";
import { ModuleIssues } from "./ModuleIssues";
import { ModuleStatus } from "./ModuleStatus";
import { getModuleStatusPresentation } from "./moduleStatusPresentation";
import {
  type HashboardReading,
  ModuleIssuesSchema,
} from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import type { HashboardSlot } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { BoardPoolConfigurationCell } from "@/protoFleet/features/poolConfigurations/BoardPoolConfigurationCell";
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

export function HashboardIssues({
  board,
  position,
  name,
  iconOnly = false,
  renderTrigger,
}: {
  board: HashboardReading;
  position: number;
  name: string;
  iconOnly?: boolean;
  now?: number;
  renderTrigger?: (open: () => void, hasIssues: boolean) => ReactNode;
}) {
  const items = board.issues.map((issue) => {
    // Classification comes from Fleet; raw diagnostics retain vendor stack detail.
    const raw = [board.health?.errors, board.health?.warnings].flatMap((value) => (Array.isArray(value) ? value : []));
    const match = raw.find(
      (value) => value && typeof value === "object" && "cause" in value && value.cause === issue.cause,
    );
    const stack = match && typeof match === "object" && "stack" in match ? match.stack : undefined;
    return typeof stack === "number" || typeof stack === "string"
      ? { ...issue, cause: `${issue.cause} (stack ${stack})` }
      : issue;
  });
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
      statusKnown={status.label !== "Unknown"}
      statusCircle={status.indicator}
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

export function HashboardCell({
  slot,
  column,
  deviceIdentifier,
}: {
  slot: HashboardSlot;
  column: string;
  deviceIdentifier?: string;
}) {
  const unit = useTemperatureUnit();
  const board = slot.occupancy === "occupied" ? slot.board : undefined;
  const identity = board?.serial || board?.macAddress;
  const name = `Hashboard ${slot.position}${identity ? ` - ${identity}` : ""}`;
  switch (column) {
    case "poolConfiguration":
      return slot.board?.hashboardId ? (
        <BoardPoolConfigurationCell hashboardId={slot.board.hashboardId} />
      ) : (
        <span>—</span>
      );
    case "name":
      return (
        <div className="flex items-center justify-between gap-2 pl-8">
          <span className="truncate" title={name}>
            {name}
          </span>
          {deviceIdentifier ? <HashboardPoolAction deviceIdentifier={deviceIdentifier} slot={slot} /> : null}
        </div>
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
      if (board.isolated)
        return (
          <span className="flex gap-2">
            <ModuleStatus state={board.state} /> · Isolated
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
