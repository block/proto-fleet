import FanSpeedRow from "./FanSpeedRow";
import { FanInfo } from "@/protoOS/api/types";

import { variants } from "@/shared/components/Button";

import Header from "@/shared/components/Header";
import Modal from "@/shared/components/Modal";
import { getDisplayValue } from "@/shared/utils/stringUtils";

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
        {/* TODO: show this when we have the value from API */}
        {/* <div>
          Fan speeds can vary but generally should be around{" "}
          {getDisplayValue(acceptableSpeed)} RPM for optimal hash rates.
        </div> */}
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
