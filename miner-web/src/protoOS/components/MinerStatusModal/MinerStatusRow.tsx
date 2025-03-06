import { useEffect, useState } from "react";

import { getErrorMessage, isError, isWarning } from "./utility";
import { NotificationError } from "@/protoOS/api/types";
import { iconSizes } from "@/shared/assets/icons/constants";
import Row from "@/shared/components/Row";
import StatusCircle, {
  type StatusCircleProps,
} from "@/shared/components/StatusCircle";

interface MinerStatusRowProps {
  error?: NotificationError;
  label?: string;
}

const MinerStatusRow = ({ error, label }: MinerStatusRowProps) => {
  const [status, setStatus] = useState<StatusCircleProps["status"]>("normal");

  useEffect(() => {
    if (isError(error?.error_level)) {
      setStatus("error");
    } else if (isWarning(error?.error_level)) {
      setStatus("warning");
    } else {
      setStatus("normal");
    }
  }, [error]);

  return (
    <Row
      prefixIcon={<StatusCircle width={iconSizes.medium} status={status} />}
      className="text-emphasis-300"
      compact
    >
      {label || getErrorMessage(error)}
    </Row>
  );
};

export default MinerStatusRow;
