import { useState } from "react";
import { ProtoOSStatusModal as StatusModal } from "@/protoOS/components/StatusModal";
import Card from "@/protoOS/features/diagnostic/components/Card";
import CardHeader from "@/protoOS/features/diagnostic/components/CardHeader";
import { useErrorsByComponent, useMinerFan } from "@/protoOS/store";
import { Alert, FanIndicatorV2 as FanIndicator } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import FanValue from "@/shared/components/FanValue";

interface FanStatusCardProps {
  fanId: number;
}

function FanStatusCard({ fanId }: FanStatusCardProps) {
  // Fetch data directly from store
  const fanData = useMinerFan(fanId);
  const [showComponentStatusModal, setShowComponentStatusModal] = useState(false);

  // Compute display values
  const rpm = fanData?.rpm?.latest?.value ?? 0;
  const pwm = fanData?.percentage?.latest?.value ?? 0;
  const position = fanData?.slot ?? fanId;
  const name = `Fan ${position}`;

  const errors = useErrorsByComponent("FAN", fanId);
  const hasErrors = errors.length > 0;

  return (
    <Card>
      <CardHeader
        title={name}
        statusIcon={hasErrors ? <Alert className="text-intent-critical-fill" width={iconSizes.small} /> : null}
        componentIcon={<FanIndicator position={position} />}
        onInfoIconClick={() => setShowComponentStatusModal(true)}
      />

      <div>
        <div className="text-emphasis-300 text-text-primary">
          <FanValue value={rpm} type="rpm" />
        </div>
        <div className="text-300 text-text-primary-70">
          <FanValue value={pwm} type="pwm" />
        </div>
      </div>
      {showComponentStatusModal && (
        <StatusModal
          show={showComponentStatusModal}
          onClose={() => setShowComponentStatusModal(false)}
          componentAddress={{
            source: "FAN",
            componentIndex: fanId - 1,
          }}
          showBackButton={false}
        />
      )}
    </Card>
  );
}

export default FanStatusCard;
