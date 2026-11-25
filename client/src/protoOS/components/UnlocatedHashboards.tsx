import ContainerMappingSetup from "@/protoOS/features/feedMapping/MappingSetup";
import { boardStatus, statusLabels } from "@/protoOS/features/tanks/tankView";
import { usePsuObservationTime } from "@/protoOS/hooks/usePsuObservationTime";
import { useDetailedErrors } from "@/protoOS/store/hooks/useMinerStatus";
import { useTemperatureUnit } from "@/protoOS/store/hooks/useUI";
import useMinerStore from "@/protoOS/store/useMinerStore";
import UnlocatedHashboards from "@/shared/components/UnlocatedHashboards";
import {
  convertCtoF,
  formatEfficiencyOrDash,
  formatHashrateOrDash,
  formatPowerKwOrDash,
} from "@/shared/utils/telemetryFormat";

export default function ContainerUnlocatedHashboards({
  stale = false,
  withMapping = false,
}: {
  stale?: boolean;
  withMapping?: boolean;
}) {
  const hardware = useMinerStore((state) => state.hardware);
  const readings = useMinerStore((state) => state.telemetry.hashboards);
  const errors = useDetailedErrors();
  const unit = useTemperatureUnit();
  const topology = hardware.topology;
  const now = usePsuObservationTime(topology?.kind === "container");
  const updatedAt = "updatedAtMs" in hardware.resource ? hardware.resource.updatedAtMs : undefined;
  if (topology?.kind !== "container") return null;
  const boards = [...topology.hashboards.values()]
    .filter((board) => !board.moduleId)
    .map((board) => {
      const reading = readings.get(board.id);
      const details = hardware.hashboards.get(board.id);
      const issues = errors.filter((error) => error.component?.id === board.id).map((error) => error.message);
      return {
        id: board.id,
        identity: board.identity.value,
        serial: details?.serialNumber,
        firmware: details?.firmware?.version,
        status: statusLabels[boardStatus(reading)],
        power: formatPowerKwOrDash(reading?.power?.latest?.value == null ? null : reading.power.latest.value / 1000),
        efficiency: formatEfficiencyOrDash(reading?.efficiency?.latest?.value ?? null),
        hashrate: formatHashrateOrDash(reading?.hashrate?.latest?.value ?? null),
        temperature:
          reading?.temperature?.latest?.value == null
            ? "—"
            : `${(unit === "F" ? convertCtoF(reading.temperature.latest.value) : reading.temperature.latest.value).toFixed(1)} °${unit}`,
        issues: issues.length ? issues : reading?.reportedIssueCount === 0 ? [] : undefined,
      };
    });
  const locationStale =
    stale ||
    updatedAt === undefined ||
    now - updatedAt > 120_000 ||
    hardware.resource.status === "stale" ||
    hardware.resource.status === "error";
  return (
    <UnlocatedHashboards
      boards={boards}
      renderCallout={
        withMapping
          ? (callout, onView) => (
              <ContainerMappingSetup
                unlocated={{ count: boards.length, stale: locationStale, onView }}
                fallback={callout}
              />
            )
          : undefined
      }
      stale={locationStale}
    />
  );
}
