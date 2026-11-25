import { useState } from "react";
import { create } from "@bufbuild/protobuf";
import { HashboardCell, HashboardIssues } from "../HashboardCell";
import HashboardPoolAction from "../HashboardPoolAction";
import { isCurrentJsonObservation, isCurrentObservation } from "../moduleDetail";
import {
  HashboardReadingSchema,
  type HashboardSlot,
} from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { BoardPoolConfigurationCell } from "@/protoFleet/features/poolConfigurations/BoardPoolConfigurationCell";
import { useTemperatureUnit } from "@/protoFleet/store";
import { Alert } from "@/shared/assets/icons";
import HashboardCard from "@/shared/components/HashboardCard";
import {
  convertCtoF,
  formatEfficiencyOrDash,
  formatHashrateOrDash,
  formatPowerKwOrDash,
} from "@/shared/utils/telemetryFormat";

export default function ModuleHashboardCard({
  slot,
  now,
  deviceIdentifier,
}: {
  slot: HashboardSlot;
  now: number;
  deviceIdentifier?: string;
}) {
  const unit = useTemperatureUnit();
  const occupied = slot.occupancy === "occupied" && isCurrentObservation(slot.observedAt, now);
  const board = occupied ? slot.board : undefined;
  const [hasDetails, setHasDetails] = useState(false);
  if (board && !hasDetails) setHasDetails(true);
  const current = board && isCurrentObservation(board.observedAt, now);
  const stateCurrent = board && isCurrentObservation(board.stateObservedAt, now);
  const reading = board
    ? {
        ...board,
        state: stateCurrent ? board.state : "unknown",
        hashrateHs: current ? board.hashrateHs : undefined,
        powerW: current ? board.powerW : undefined,
        efficiencyJh: current ? board.efficiencyJh : undefined,
        temperatureC: current ? board.temperatureC : undefined,
        health: current && stateCurrent ? board.health : undefined,
        // Issues carry independent source timestamps; ModuleIssues expires each cause.
        issues: board.issues,
      }
    : create(HashboardReadingSchema, { state: "unknown" });
  // The plugin preserves this optional MDK field in the collected health payload.
  // Its ASIC sample can expire independently of the overall board reading.
  const high =
    current && isCurrentJsonObservation(board.health?.asicTemperatureObservedAt, now)
      ? board.health?.maximumAsicTemperatureC
      : undefined;
  const temperature = (value: unknown) =>
    typeof value === "number" && Number.isFinite(value)
      ? `${(unit === "F" ? convertCtoF(value) : value).toFixed(1)} °${unit}`
      : "—";
  const metrics = [
    {
      label: "Efficiency",
      value: formatEfficiencyOrDash(reading?.efficiencyJh === undefined ? null : reading.efficiencyJh * 1e12),
    },
    {
      label: "Total hashrate",
      value: formatHashrateOrDash(reading?.hashrateHs === undefined ? null : reading.hashrateHs / 1e12),
    },
    { label: "Total power", value: formatPowerKwOrDash(reading?.powerW === undefined ? null : reading.powerW / 1000) },
    { label: "ASIC avg", value: temperature(reading?.temperatureC) },
    { label: "ASIC high", value: temperature(high) },
  ];
  const name = `Board ${slot.position}`;
  const renderCard = (onInfoClick?: () => void, hasIssues = false) => (
    <HashboardCard
      title={name}
      position={slot.position}
      metrics={board ? metrics : []}
      onInfoClick={onInfoClick}
      statusIcon={
        hasIssues ? (
          <span role="img" aria-label={`${name} needs attention`}>
            <Alert width="w-4" className="text-intent-critical-fill" />
          </span>
        ) : null
      }
    >
      {slot.board?.hashboardId ? <BoardPoolConfigurationCell hashboardId={slot.board.hashboardId} /> : null}
      {deviceIdentifier ? (
        <div className="flex justify-end">
          <HashboardPoolAction deviceIdentifier={deviceIdentifier} slot={slot} />
        </div>
      ) : null}
      {board ? (
        !current ? (
          <p role="status" className="text-200 text-text-primary-70">
            Board readings unavailable or stale.
          </p>
        ) : null
      ) : (
        <div className="text-200 text-text-primary-70">
          <HashboardCell
            slot={{
              ...slot,
              occupancy: isCurrentObservation(slot.observedAt, now) ? slot.occupancy : "unknown",
              board: undefined,
            }}
            column="status"
          />
        </div>
      )}
    </HashboardCard>
  );
  return (
    <HashboardIssues
      board={reading}
      position={slot.position}
      name={name}
      now={now}
      renderTrigger={(open, hasIssues) => renderCard(board || hasDetails ? open : undefined, hasIssues)}
    />
  );
}
