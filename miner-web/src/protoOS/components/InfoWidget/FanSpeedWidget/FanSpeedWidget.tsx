import { useMemo, useState } from "react";

import FanSpeedModal from "./FanSpeedModal";
import FanSpeedPieChart from "./FanSpeedPieChart";
import { FanInfo } from "@/protoOS/api/types";
import InfoWidget from "@/protoOS/components/InfoWidget";
import { getDisplayValue } from "@/shared/utils/stringUtils";

interface FanSpeedWidgetProps {
  fanSpeeds?: FanInfo[];
  loading?: boolean;
}

const FanSpeedWidget = ({ fanSpeeds, loading }: FanSpeedWidgetProps) => {
  const [showModal, setShowModal] = useState(false);
  // TODO: get max speed from API
  const maxSpeed = 6000;
  // TODO: get acceptable speed from API
  const acceptableSpeed = maxSpeed / 2;

  const avgFanSpeed = useMemo(() => {
    if (!fanSpeeds?.length) {
      return;
    }

    const total = fanSpeeds.reduce((acc, fan) => acc + (fan.rpm || 0), 0);
    return Math.round(total / fanSpeeds.length);
  }, [fanSpeeds]);

  const displayFanSpeed = useMemo(
    () => avgFanSpeed && `${getDisplayValue(avgFanSpeed)} RPM`,
    [avgFanSpeed],
  );

  return (
    <>
      <InfoWidget
        title="Current avg. fan speed"
        value={displayFanSpeed}
        loading={loading}
        hasBorder
        onClick={loading ? undefined : () => setShowModal(true)}
        wrapperClassName="phone:flex-col phone:space-y-4"
        stats={
          !loading && (
            <div className="flex h-[60px] w-full justify-end space-x-3 phone:justify-normal">
              {fanSpeeds?.map(
                (fan, index) =>
                  fan.rpm !== undefined && (
                    <div
                      className="flex h-full w-10 flex-col items-center"
                      key={index}
                    >
                      <div className="mb-1 font-mono text-mono-text-50 text-text-primary-50">
                        F{index + 1}
                      </div>
                      <FanSpeedPieChart
                        key={index}
                        acceptableSpeed={acceptableSpeed}
                        fanSpeed={fan.rpm}
                        maxSpeed={maxSpeed}
                      />
                    </div>
                  ),
              )}
            </div>
          )
        }
      />
      {showModal && (
        <FanSpeedModal
          acceptableSpeed={acceptableSpeed}
          fanSpeeds={fanSpeeds}
          maxSpeed={maxSpeed}
          onDismiss={() => setShowModal(false)}
        />
      )}
    </>
  );
};

export default FanSpeedWidget;
