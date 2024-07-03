import { FanInfo } from "apiTypes";

import { getDisplayValue } from "common/utils/stringUtils";

import { variants } from "components/Button";
import Header from "components/Header";
import Modal from "components/Modal";

import FanSpeedRow from "./FanSpeedRow";

interface FanSpeedModalProps {
  acceptableSpeed: number;
  fanSpeeds?: FanInfo[];
  maxSpeed: number;
  onDismiss: () => void;
}

const FanSpeedModal = ({
  acceptableSpeed,
  fanSpeeds,
  maxSpeed,
  onDismiss,
}: FanSpeedModalProps) => {
  return (
    <Modal
      buttons={[
        // TODO: add onClick handler when detailed documentation is ready
        // {
        //   text: "View Details",
        //   onClick: handleClickViewDetails,
        //   variant: variants.secondary,
        // },
        {
          text: "Done",
          variant: variants.primary,
        },
      ]}
      contentHeader="Fan speed"
      onDismiss={onDismiss}
    >
      <div className="space-y-6">
        <div>
          Fan speeds can vary but generally should be around{" "}
          {getDisplayValue(acceptableSpeed)} RPM for optimal hash rates.
        </div>
        <div>
          <Header title="Current fan speeds" titleSize="text-heading-50" />
          {fanSpeeds?.map((fan, index) => {
            const speed = fan.rpm || 0;
            const lowSpeed = speed < acceptableSpeed;
            return (
              <FanSpeedRow
                label={`Fan ${index + 1}`}
                secondaryLabel={`${lowSpeed ? "Low speed • " : ""}${getDisplayValue(speed)} RPM`}
                warn={lowSpeed}
                acceptableSpeed={acceptableSpeed}
                maxSpeed={maxSpeed}
                speed={speed}
                divider={index + 1 !== fanSpeeds.length}
                key={index}
              />
            );
          })}
        </div>
      </div>
    </Modal>
  );
};

export default FanSpeedModal;
