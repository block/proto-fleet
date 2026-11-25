import { useState } from "react";
import { isCurrentObservation } from "../moduleDetail";
import { ModuleIssues } from "../ModuleIssues";
import { modulePsu, psuNeedsRecovery } from "../modulePsu";
import PsuFirmwareStatus from "../PsuFirmwareStatus";
import type { HashboardSlot, InventoryItem } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { useTemperatureUnit } from "@/protoFleet/store";
import { Alert } from "@/shared/assets/icons";
import PsuCard from "@/shared/components/PsuCard";
import ComponentMetadata from "@/shared/components/StatusModal/ComponentMetadata";
import { convertCtoF } from "@/shared/utils/telemetryFormat";

export default function ModulePsuCard({
  module,
  slots,
  unavailable,
  now,
}: {
  module: InventoryItem;
  slots: HashboardSlot[];
  unavailable: boolean;
  now: number;
}) {
  const unit = useTemperatureUnit();
  const psu = modulePsu(unavailable ? [] : slots, module.ref!.id, now);
  const evidence = unavailable ? undefined : module.moduleIssues;
  const issues =
    evidence?.items.filter((issue) => issue.component === "psu" && isCurrentObservation(issue.observedAt, now)) ?? [];
  const available = !!psu || issues.length > 0;
  const needsRecovery = psuNeedsRecovery(psu?.firmwareUpdate);
  const [hasDetails, setHasDetails] = useState(false);
  if (available && !hasDetails) setHasDetails(true);
  const temperature = (value: number | undefined) =>
    value === undefined ? "—" : `${(unit === "F" ? convertCtoF(value) : value).toFixed(1)} °${unit}`;
  const voltage = (value: number | undefined) => (value === undefined ? "—" : `${value.toFixed(1)} V`);
  const power = (value: number | undefined) => (value === undefined ? "—" : `${(value / 1000).toFixed(1)} kW`);
  const metrics = [
    { label: "Input voltage", value: voltage(psu?.inputVoltage) },
    { label: "Input power", value: power(psu?.inputPower) },
    { label: "Output voltage", value: voltage(psu?.outputVoltage) },
    { label: "Output power", value: power(psu?.outputPower) },
    { label: "Avg temp", value: temperature(psu?.averageTemperature) },
    { label: "High temp", value: temperature(psu?.highTemperature) },
  ];
  const metadata = Object.fromEntries(
    [
      ["serial", "Serial number"],
      ["vendor", "Manufacturer"],
      ["model", "Model"],
      ["revision", "Hardware revision"],
      ["firmwareVersion", "Firmware"],
    ].map(([key, label]) => {
      const value = psu?.identity?.[key];
      return [key, { label, value: typeof value === "string" && value ? value : undefined }];
    }),
  );
  return (
    <ModuleIssues
      item={{ name: "PSU 1", moduleIssues: available && evidence ? { ...evidence, items: issues } : undefined }}
      status="All components operational"
      renderTrigger={(open, hasIssues) =>
        available || hasDetails ? (
          <>
            <PsuCard
              title="PSU 1"
              metrics={metrics}
              onInfoClick={open}
              infoAriaLabel="PSU 1 info"
              statusIcon={
                hasIssues || needsRecovery ? (
                  <span role="img" aria-label="PSU 1 needs attention">
                    <Alert width="w-4" className="text-intent-critical-fill" />
                  </span>
                ) : null
              }
            />
            {!available ? <p role="status">No PSU data available.</p> : null}
          </>
        ) : (
          <p role="status">No PSU data available.</p>
        )
      }
    >
      <ComponentMetadata metadata={metadata} />
      <PsuFirmwareStatus update={psu?.firmwareUpdate} now={now} />
    </ModuleIssues>
  );
}
