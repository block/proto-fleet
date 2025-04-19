import { ReactNode, useEffect, useState } from "react";

import R2Status from "./R2Status";
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
  icon?: ReactNode;
}

// TODO: once api is available for system model
// we should keep this at a context that wraps the entire app
// for now we can just assume R2
const isR2 = true;

const MinerStatusRow = ({ error, label, icon }: MinerStatusRowProps) => {
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
      prefixIcon={
        icon && isR2 ? (
          <R2Status icon={icon} status={status} />
        ) : (
          <StatusCircle width={iconSizes.medium} status={status} />
        )
      }
      className="text-emphasis-300"
      compact
    >
      {label || getErrorMessage(error)}
    </Row>
  );
};

export default MinerStatusRow;
