import { addCommas } from "common/utils/stringUtils";

import Row from "components/Row";

import { arrayOfWarnings } from "../utility";
import FanSpeedPieChart from "./FanSpeedPieChart";

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
        <Row key={index} divider={index + 1 !== numberOfErrors} className="flex">
          <div className="mr-4 w-10 h-10">
            {/* TODO: get max speed from API */}
            <FanSpeedPieChart fanSpeed={fanSpeeds[index]} maxSpeed={3500} />
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
