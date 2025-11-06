import { useState } from "react";
import { useNavigate } from "react-router-dom";
import HashboardInfoModal from "./HashboardInfoModal";
import Card from "@/protoOS/features/diagnostic/components/Card";
import CardHeader from "@/protoOS/features/diagnostic/components/CardHeader";
import LabeledValue from "@/protoOS/features/diagnostic/components/LabeledValue";
import {
  useAsicDataTransform,
  useHashboardSlot,
  useMinerHashboard,
  useMinerHashboardAsics,
} from "@/protoOS/store";
import { HashboardIndicatorV2 as HashboardIndicator } from "@/shared/assets/icons";
import AsicTablePreview from "@/shared/components/AsicTablePreview";
import Button from "@/shared/components/Button";
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
  const [isModalOpen, setIsModalOpen] = useState(false);

  // Transform protoOS asic data to shared component format
  const asicData = useAsicDataTransform(asics);

  const handleViewClick = () => {
    navigate(`${serialNumber}`);
  };

  // Compute display values from store data
  const avgAsicTemp = hashboardData?.avgAsicTemp?.latest?.value ?? undefined;
  const maxAsicTemp = hashboardData?.maxAsicTemp?.latest?.value ?? undefined;
  const position = slot || 0;
  const name = `Hashboard ${slot}`;
  // TODO: Add hasWarning logic based on error state

  return (
    <Card>
      <CardHeader
        title={name}
        statusIcon={null /* TODO: Add warning icon based on error state */}
        componentIcon={<HashboardIndicator width="w-4" position={position} />}
        onInfoIconClick={() => setIsModalOpen(true)}
        actions={
          <Button variant="secondary" size="compact" onClick={handleViewClick}>
            View
          </Button>
        }
      />

      <div className="flex flex-col gap-3">
        <div className="grid grid-cols-2 gap-x-4 gap-y-3">
          <LabeledValue
            value={<TemperatureValue value={avgAsicTemp} />}
            label="ASIC avg"
          />
          <LabeledValue
            value={<TemperatureValue value={maxAsicTemp} />}
            label="Asic high"
          />
        </div>
        <AsicTablePreview asics={asicData} />
      </div>
      {isModalOpen && (
        <HashboardInfoModal
          serial={serialNumber}
          onDismiss={() => setIsModalOpen(false)}
        />
      )}
    </Card>
  );
}

export default HashboardStatusCard;
