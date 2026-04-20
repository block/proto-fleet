import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { ProtoOSStatusModal as StatusModal } from "@/protoOS/components/StatusModal";
import Card from "@/protoOS/features/diagnostic/components/Card";
import CardHeader from "@/protoOS/features/diagnostic/components/CardHeader";
import LabeledValue from "@/protoOS/features/diagnostic/components/LabeledValue";
import {
  useAsicDataTransform,
  useErrorsByComponent,
  useHashboardSlot,
  useMinerHashboard,
  useMinerHashboardAsics,
} from "@/protoOS/store";
import { Alert, HashboardIndicatorV2 as HashboardIndicator } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import AsicTablePreview from "@/shared/components/AsicTablePreview";
import Button from "@/shared/components/Button";
import HashRateValue from "@/shared/components/HashRateValue";
import PowerValue from "@/shared/components/PowerValue";
import TemperatureValue from "@/shared/components/TemperatureValue";

interface HashboardStatusCardProps {
  serialNumber: string;
}

function HashboardStatusCard({ serialNumber }: HashboardStatusCardProps) {
  // Fetch data directly from store
  const hashboardData = useMinerHashboard(serialNumber);
  const slot = useHashboardSlot(serialNumber);
  const asics = useMinerHashboardAsics(serialNumber);
  const navigate = useNavigate();
  const [showComponentStatusModal, setShowComponentStatusModal] = useState(false);

  // Transform protoOS asic data to shared component format
  const asicData = useAsicDataTransform(asics);

  const handleViewClick = () => {
    navigate(`${serialNumber}`);
  };

  // Compute display values from store data
  const avgAsicTemp = hashboardData?.avgAsicTemp?.latest?.value ?? undefined;
  const maxAsicTemp = hashboardData?.maxAsicTemp?.latest?.value ?? undefined;
  const power = hashboardData?.power?.latest?.value ?? undefined;
  const hashrate = hashboardData?.hashrate?.latest?.value ?? undefined;
  const position = slot || 0;
  const name = `Slot ${slot}`;

  const errors = useErrorsByComponent("HASHBOARD", slot ?? 1);
  const hasErrors = errors.length > 0;

  return (
    <Card>
      <CardHeader
        title={name}
        statusIcon={hasErrors ? <Alert className="text-intent-critical-fill" width={iconSizes.small} /> : null}
        componentIcon={<HashboardIndicator width="w-4" position={position} />}
        onInfoIconClick={() => setShowComponentStatusModal(true)}
        actions={
          <Button variant="secondary" size="compact" onClick={handleViewClick}>
            View
          </Button>
        }
      />

      <div className="flex flex-col gap-3">
        <div className="grid grid-cols-2 gap-x-4 gap-y-3">
          <LabeledValue value={<TemperatureValue value={avgAsicTemp} />} label="ASIC avg" />
          <LabeledValue value={<TemperatureValue value={maxAsicTemp} />} label="Asic high" />
          <LabeledValue value={<PowerValue value={power} />} label="Power" />
          <LabeledValue value={<HashRateValue value={hashrate} />} label="Hashrate" />
        </div>
        <AsicTablePreview asics={asicData} />
      </div>
      <StatusModal
        open={showComponentStatusModal}
        onClose={() => setShowComponentStatusModal(false)}
        componentAddress={{
          source: "HASHBOARD",
          slot: slot ?? 1,
        }}
        showBackButton={false}
      />
    </Card>
  );
}

export default HashboardStatusCard;
