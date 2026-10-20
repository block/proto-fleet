import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { ProtoOSStatusModal as StatusModal } from "@/protoOS/components/StatusModal";
import { useHashboardLayout } from "@/protoOS/features/diagnostic/hashboardLayout";
import {
  useAsicDataTransform,
  useHashboardSlot,
  useMinerHashboard,
  useMinerHashboardAsics,
  useMinerStore,
} from "@/protoOS/store";
import { useErrorsByRef } from "@/protoOS/store/hooks/useMinerStatus";
import { efficiencyValue } from "@/protoOS/store/utils/efficiency";
import { getHashboardId } from "@/protoOS/store/utils/entityIds";
import { Alert } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import AsicTablePreview from "@/shared/components/AsicTablePreview";
import Button from "@/shared/components/Button";
import EfficiencyValue from "@/shared/components/EfficiencyValue";
import HashboardCard from "@/shared/components/HashboardCard";
import HashRateValue from "@/shared/components/HashRateValue";
import PowerValue from "@/shared/components/PowerValue";
import TemperatureValue from "@/shared/components/TemperatureValue";

interface HashboardStatusCardProps {
  /** Board identity value: CT1 MAC address or R2 serial number. */
  identifier: string;
  viewTo?: string;
}

function HashboardStatusCard({ identifier, viewTo }: HashboardStatusCardProps) {
  // Fetch data directly from store
  const hashboardData = useMinerHashboard(identifier);
  const slot = useHashboardSlot(identifier);
  const asics = useMinerHashboardAsics(identifier);
  const navigate = useNavigate();
  const [showComponentStatusModal, setShowComponentStatusModal] = useState(false);
  const layout = useHashboardLayout();

  // Transform protoOS asic data to shared component format
  const asicData = useAsicDataTransform(asics);

  const handleViewClick = () => {
    navigate(viewTo ?? encodeURIComponent(identifier));
  };

  // Compute display values from store data
  const avgAsicTemp = hashboardData?.averageAsicTemperature?.latest?.value ?? undefined;
  const maxAsicTemp = hashboardData?.maximumAsicTemperature?.latest?.value ?? undefined;
  const power = hashboardData?.power?.latest?.value ?? undefined;
  const hashrate = hashboardData?.hashrate?.latest?.value ?? undefined;
  const position = slot || 0;
  const name = layout.card.title(slot);

  const component = useMinerStore((state) => state.hardware.hashboards.get(getHashboardId(identifier))?.ref);
  const errors = useErrorsByRef(component);
  const hasErrors =
    errors.length > 0 ||
    hashboardData?.health === "fault" ||
    hashboardData?.health === "degraded" ||
    (hashboardData?.reportedIssueCount ?? 0) > 0;

  const temperatures = [
    { label: "ASIC avg", value: avgAsicTemp === undefined ? "—" : <TemperatureValue value={avgAsicTemp} /> },
    { label: "ASIC high", value: maxAsicTemp === undefined ? "—" : <TemperatureValue value={maxAsicTemp} /> },
  ];
  const rateStat = {
    label: layout.card.showEfficiency ? "Total hashrate" : "Hashrate",
    value: hashrate === undefined ? "—" : <HashRateValue value={hashrate} />,
  };
  const powerStat = {
    label: layout.card.showEfficiency ? "Total power" : "Power",
    value:
      power === undefined ? (
        "—"
      ) : layout.card.showEfficiency ? (
        `${(power / 1000).toFixed(1)} kW`
      ) : (
        <PowerValue value={power} />
      ),
  };
  const efficiency = efficiencyValue(power, hashrate, hashboardData?.efficiency?.latest?.value);
  const stats = layout.card.showEfficiency
    ? [
        { label: "Efficiency", value: efficiency === null ? "—" : <EfficiencyValue value={efficiency} /> },
        rateStat,
        powerStat,
        ...temperatures,
      ]
    : [...temperatures, powerStat, rateStat];

  return (
    <>
      <HashboardCard
        title={name}
        statusIcon={
          hasErrors ? (
            <span role="img" aria-label={`${name} needs attention`}>
              <Alert className="text-intent-critical-fill" width={iconSizes.small} />
            </span>
          ) : null
        }
        position={layout.card.showSlotIndicator ? position : undefined}
        totalSlots={layout.card.totalSlots}
        threeColumns={layout.card.showEfficiency}
        metrics={stats}
        onInfoClick={component ? () => setShowComponentStatusModal(true) : undefined}
        actions={
          <Button variant="secondary" size="compact" onClick={handleViewClick}>
            View
          </Button>
        }
      >
        <AsicTablePreview asics={asicData} rowHeight={layout.card.previewRowHeight} />
      </HashboardCard>
      {showComponentStatusModal && component ? (
        <StatusModal
          open={showComponentStatusModal}
          onClose={() => setShowComponentStatusModal(false)}
          componentAddress={component}
          showBackButton={false}
        />
      ) : null}
    </>
  );
}

export default HashboardStatusCard;
