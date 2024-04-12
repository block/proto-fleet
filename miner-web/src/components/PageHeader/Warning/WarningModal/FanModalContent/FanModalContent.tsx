import { addCommas } from "common/utils/stringUtils";

import FanSpeedPieChart from "components/InfoWidget/FanSpeedWidget/FanSpeedPieChart";
import Row from "components/Row";

import { arrayOfWarnings } from "../utility";

const FanModalContent = () => {
  // TODO: get number of errors from API
  const numberOfErrors = 2;
  // TODO: get speed from API
  const fanSpeeds = [3049, 3240];

  return (
    <>
      <div className="mb-6">
        <div className="text-heading-200 text-text-primary mb-1">
          Fan issues
        </div>
        <div className="text-300 text-text-primary/70">
          {numberOfErrors} fans are underperforming which may impact your hash
          rate.
        </div>
      </div>
      {arrayOfWarnings(numberOfErrors).map((_, index) => (
        <Row
          key={index}
          divider={index + 1 !== numberOfErrors}
          className="flex"
        >
          <div className="mr-4 w-10 h-10">
            {/* TODO: get acceptable speed and max speed from API */}
            <FanSpeedPieChart
              acceptableSpeed={6500}
              fanSpeed={fanSpeeds[index]}
              maxSpeed={7500}
            />
          </div>
          <div>
            <div className="text-emphasis-300 text-text-primary">
              Fan {index + 1}
            </div>
            <div className="text-200 text-intent-warning-text">
              Low speed • {addCommas(fanSpeeds[index])} RPM
            </div>
          </div>
        </Row>
      ))}
    </>
  );
};

export default FanModalContent;
