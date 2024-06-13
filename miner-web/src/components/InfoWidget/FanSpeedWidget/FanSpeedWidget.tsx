import { useMemo, useState } from "react";

import { FanInfo } from "apiTypes";

import { getDisplayValue } from "common/utils/stringUtils";

import InfoWidget from "components/InfoWidget";

import FanSpeedModal from "./FanSpeedModal";
import FanSpeedPieChart from "./FanSpeedPieChart";

interface FanSpeedWidgetProps {
  fanSpeeds?: FanInfo[];
  loading?: boolean;
}

const FanSpeedWidget = ({ fanSpeeds, loading }: FanSpeedWidgetProps) => {
  const [showModal, setShowModal] = useState(false);
  // TODO: get max speed from API
  const maxSpeed = 3000;
  // TODO: get acceptable speed from API
  const acceptableSpeed = 2800;

  const avgFanSpeed = useMemo(() => {
    if (!fanSpeeds?.length) {
      return;
    }

    const total = fanSpeeds.reduce((acc, fan) => acc + (fan.rpm || 0), 0);
    return total / fanSpeeds.length;
  }, [fanSpeeds]);

  const displayFanSpeed = useMemo(
    () => avgFanSpeed && `${getDisplayValue(avgFanSpeed)} RPM`,
    [avgFanSpeed]
  );

  return (
    <>
      <InfoWidget
        title="Avg. fan speed"
        value={displayFanSpeed}
        loading={loading}
        hasBorder
        onClick={loading ? undefined : () => setShowModal(true)}
        wrapperClassName="phone:flex-col phone:space-y-4"
        stats={
          !loading && (
            <div className="flex w-full h-[60px] space-x-3 justify-end phone:justify-normal">
              {fanSpeeds?.map(
                (fan, index) =>
                  fan.rpm !== undefined && (
                    <div
                      className="flex flex-col w-10 h-full items-center"
                      key={index}
                    >
                      <div className="text-mono-text-50 text-text-primary/50 mb-1">
                        F{index + 1}
                      </div>
                      <FanSpeedPieChart
                        key={index}
                        acceptableSpeed={acceptableSpeed}
                        fanSpeed={fan.rpm}
                        maxSpeed={maxSpeed}
                      />
                    </div>
                  )
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
