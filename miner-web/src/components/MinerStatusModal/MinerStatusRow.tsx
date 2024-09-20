import { NotificationError } from "apiTypes";

import Row from "components/Row";

import { iconSizes } from "icons/constants";

import { ErrorLevel } from "./constants";
import StatusCircle from "./StatusCircle";
import { getErrorMessage } from "./utility";

interface MinerStatusRowProps {
  error?: NotificationError;
  label?: string;
}

const MinerStatusRow = ({ error, label }: MinerStatusRowProps) => (
  <Row
    prefixIcon={
      <StatusCircle
        width={iconSizes.medium}
        isWarning={error?.error_level === ErrorLevel.warning}
        isError={error?.error_level === ErrorLevel.error}
      />
    }
    className="text-emphasis-300"
    compact
  >
    {label || getErrorMessage(error)}
  </Row>
);

export default MinerStatusRow;
