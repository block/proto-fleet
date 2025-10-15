import { useState } from "react";
import { useNavigate } from "react-router-dom";
import HashboardInfoModal from "./HashboardInfoModal";
import Card from "@/protoOS/features/diagnostic/components/Card";
import CardHeader from "@/protoOS/features/diagnostic/components/CardHeader";
import LabeledValue from "@/protoOS/features/diagnostic/components/LabeledValue";
import AsicTablePreview from "@/protoOS/features/kpis/components/Temperature/HbTempPreview/AsicTablePreview";
import {
  Alert,
  HashboardIndicatorV2 as HashboardIndicator,
} from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
import TemperatureValue from "@/shared/components/TemperatureValue";

interface HashboardData {
  id: number;
  name: string;
  position: number;
  avgAsicTemp: number;
  maxAsicTemp: number;
  hasWarning?: boolean;
}

interface HashboardStatusCardProps {
  hashboardData: HashboardData;
  serialNumber?: string;
}

function HashboardStatusCard({
  hashboardData,
  serialNumber,
}: HashboardStatusCardProps) {
  const navigate = useNavigate();
  const [isModalOpen, setIsModalOpen] = useState(false);

  const handleViewClick = () => {
    navigate(`${serialNumber}`);
  };

  return (
    <Card>
      <CardHeader
        title={hashboardData.name}
        statusIcon={
          hashboardData.hasWarning ? (
            <Alert className="text-text-critical" />
          ) : null
        }
        componentIcon={
          <HashboardIndicator width="w-4" position={hashboardData.position} />
        }
        onInfoIconClick={() => setIsModalOpen(true)}
        actions={
          serialNumber ? (
            <Button
              variant="secondary"
              size="compact"
              onClick={handleViewClick}
            >
              View
            </Button>
          ) : null
        }
      />

      <div className="flex flex-col gap-3">
        <div className="grid grid-cols-2 gap-x-4 gap-y-3">
          <LabeledValue
            value={<TemperatureValue value={hashboardData.avgAsicTemp} />}
            label="ASIC avg"
          />
          <LabeledValue
            value={<TemperatureValue value={hashboardData.maxAsicTemp} />}
            label="Asic high"
          />
        </div>
        {serialNumber && <AsicTablePreview hashboardSerial={serialNumber} />}
      </div>
      {isModalOpen && serialNumber && (
        <HashboardInfoModal
          serial={serialNumber}
          onDismiss={() => setIsModalOpen(false)}
        />
      )}
    </Card>
  );
}

export default HashboardStatusCard;
