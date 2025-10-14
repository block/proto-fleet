import { useState } from "react";
import FanInfoModal from "./FanInfoModal";
import Card from "@/protoOS/features/diagnostic/components/Card";
import CardHeader from "@/protoOS/features/diagnostic/components/CardHeader";
import type { FanData } from "@/protoOS/features/diagnostic/types";
import { Alert, FanIndicatorV2 as FanIndicator } from "@/shared/assets/icons";

export type { FanData };

interface FanStatusCardProps {
  fanData: FanData;
}

function FanStatusCard({ fanData }: FanStatusCardProps) {
  const [isModalOpen, setIsModalOpen] = useState(false);
  const formatRPM = (rpm: number) => {
    return rpm.toLocaleString() + " RPM";
  };

  const formatPWM = (pwm: number) => {
    return pwm.toFixed(1) + "% PWM";
  };

  return (
    <Card>
      <CardHeader
        title={fanData.name}
        statusIcon={
          fanData.hasWarning ? <Alert className="text-text-critical" /> : null
        }
        componentIcon={<FanIndicator position={fanData.position} />}
        onInfoIconClick={() => setIsModalOpen(true)}
      />

      <div>
        <div className="text-emphasis-300 text-text-primary">
          {formatRPM(fanData.rpm)}
        </div>
        <div className="text-300 text-text-primary-70">
          {formatPWM(fanData.pwm)}
        </div>
      </div>
      {isModalOpen && (
        <FanInfoModal
          onDismiss={() => setIsModalOpen(false)}
          fanData={fanData}
        />
      )}
    </Card>
  );
}

export default FanStatusCard;
