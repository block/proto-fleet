import { useState } from "react";
import FanInfoModal from "./FanInfoModal";
import Card from "@/protoOS/features/diagnostic/components/Card";
import CardHeader from "@/protoOS/features/diagnostic/components/CardHeader";
import { useMinerFan } from "@/protoOS/store";
import { FanIndicatorV2 as FanIndicator } from "@/shared/assets/icons";
import FanValue from "@/shared/components/FanValue";

interface FanStatusCardProps {
  fanId: number;
}

function FanStatusCard({ fanId }: FanStatusCardProps) {
  // Fetch data directly from store
  const fanData = useMinerFan(fanId);
  const [isModalOpen, setIsModalOpen] = useState(false);

  // Compute display values
  const rpm = fanData?.rpm?.latest?.value ?? 0;
  const pwm = fanData?.percentage?.latest?.value ?? 0;
  const position = fanData?.slot ?? fanData?.id ?? 0;
  const name = fanData?.name ?? `Fan ${position}`;
  // TODO: Add hasWarning logic based on error state

  return (
    <Card>
      <CardHeader
        title={name}
        statusIcon={null /* TODO: Add warning icon based on error state */}
        componentIcon={<FanIndicator position={position} />}
        onInfoIconClick={() => setIsModalOpen(true)}
      />

      <div>
        <div className="text-emphasis-300 text-text-primary">
          <FanValue value={rpm} type="rpm" />
        </div>
        <div className="text-300 text-text-primary-70">
          <FanValue value={pwm} type="pwm" />
        </div>
      </div>
      {isModalOpen && fanData && (
        <FanInfoModal
          onDismiss={() => setIsModalOpen(false)}
          fanData={{
            id: fanId,
            position,
            name,
            rpm,
            pwm,
            meta: {
              serialNumber: undefined,
              manufacturer: undefined,
              model: undefined,
              firmwareVersion: undefined,
            },
          }}
        />
      )}
    </Card>
  );
}

export default FanStatusCard;
