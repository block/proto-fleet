import { Link } from "react-router-dom";

import Row from "components/Row";

import { arrayOfWarnings } from "../utility";
import AsicTempGradientBar from "./AsicTempGradientBar";

interface AsicModalContentProps {
  onDismiss: () => void;
}

const AsicModalContent = ({ onDismiss }: AsicModalContentProps) => {
  // TODO: get number of errors from API
  const numberOfErrors = 12;
  const asics = [
    { asic: "E1", hashboard: 1, temp: 81.9 },
    { asic: "E2", hashboard: 1, temp: 80.1 },
    { asic: "E3", hashboard: 1, temp: 79.2 },
    { asic: "E4", hashboard: 1, temp: 77.4 },
    { asic: "E5", hashboard: 1, temp: 74.5 },
    { asic: "E6", hashboard: 2, temp: 72.4 },
    { asic: "E7", hashboard: 2, temp: 69.5 },
    { asic: "E8", hashboard: 2, temp: 64.7 },
  ];

  return (
    <>
      <div className="mb-6">
        <div className="text-heading-200 text-text-primary mb-1">
          ASIC issues
        </div>
        <div className="text-300 text-text-primary/70">
          {numberOfErrors} chips are currently overheating which may impact your
          hash rate.
        </div>
      </div>
      <Row className="flex text-emphasis-300 text-text-primary" compact>
        <div className="w-1/3">ASIC</div>
        <div className="w-1/3">Hashboard</div>
        <div className="w-1/3">Temperature</div>
      </Row>
      {arrayOfWarnings(numberOfErrors).map((_, index) => (
        <Row key={index} divider={index + 1 !== numberOfErrors} compact>
          <div className="flex text-text-primary text-300">
            <div className="w-1/3">{asics[index].asic}</div>
            <div className="w-1/3">{asics[index].hashboard}</div>
            <div className="flex items-center w-1/3">
              <div className="grow">{asics[index].temp}º</div>
              {/* TODO: get maxTemp from API */}
              <AsicTempGradientBar temp={asics[index].temp} maxTemp={100} />
            </div>
          </div>
        </Row>
      ))}
      {numberOfErrors > 8 && (
        <Row
          className="text-emphasis-300 text-text-emphasis -mb-3"
          divider={false}
          compact
        >
          <Link to="/hardware" onClick={onDismiss}>
            View all ASICs
          </Link>
        </Row>
      )}
    </>
  );
};

export default AsicModalContent;
