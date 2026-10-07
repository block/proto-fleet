import { create } from "@bufbuild/protobuf";
import { ModuleIssues } from "./ModuleIssues";
import { ModuleStatus } from "./ModuleStatus";
import {
  type HashboardReading,
  ModuleIssuesSchema,
} from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import type { HashboardSlot } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { useTemperatureUnit } from "@/protoFleet/store";
import { formatHashrateOrDash } from "@/shared/utils/telemetryFormat";

const unavailableReasons: Record<string, string> = {
  invalid_observation: "Invalid board data",
  ambiguous_slot: "Slot position conflict",
  missing_identity: "Board identity missing",
  identity_conflict: "Board identity conflict",
  controller_conflict: "Board reported by another controller",
  older_observation: "Newer board observation exists",
};

function HashboardIssues({ board, position, name }: { board: HashboardReading; position: number; name: string }) {
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
  const evidence = create(ModuleIssuesSchema, {
    observedAt: board.observedAt,
    complete: board.health !== undefined,
    items,
  });
  return (
    <ModuleIssues item={{ name, moduleIssues: evidence }} hashboardPosition={position}>
      <p>Hashrail: {board.hashrailOk === undefined ? "Unknown" : board.hashrailOk ? "OK" : "Not ready"}</p>
      <p>Reference clocks: {board.refclksOk === undefined ? "Unknown" : board.refclksOk ? "OK" : "Not ready"}</p>
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
      if (board.state === "isolated") return <span>Isolated</span>;
      return <ModuleStatus state={board.state} />;
    case "hashrate":
      return <span>{formatHashrateOrDash(board?.hashrateHs === undefined ? null : board.hashrateHs / 1e12)}</span>;
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
