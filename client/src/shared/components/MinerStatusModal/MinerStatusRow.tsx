import { ReactNode } from "react";
import { clsx } from "clsx";

import R2Status from "./R2Status";
import { type Issue } from "./types";
import { iconSizes } from "@/shared/assets/icons/constants";
import Row from "@/shared/components/Row";
import StatusCircle, { statuses } from "@/shared/components/StatusCircle";

interface MinerStatusRowProps {
  issue?: Issue;
  icon?: ReactNode;
  componentName: string;
  disabled?: boolean;
}

// TODO: once api is available for system model
// we should keep this at a context that wraps the entire app
// for now we can just assume R2
const isR2 = true;

const MinerStatusRow = ({
  issue,
  icon,
  componentName,
  disabled = false,
}: MinerStatusRowProps) => {
  return (
    <Row
      prefixIcon={
        icon && isR2 ? (
          <R2Status
            icon={icon}
            hasIssue={issue !== undefined}
            disabled={disabled}
          />
        ) : (
          <StatusCircle
            width={iconSizes.medium}
            status={issue ? statuses.error : statuses.normal}
          />
        )
      }
      className={clsx("text-emphasis-300", { "opacity-30": disabled })}
      compact
    >
      <div className="py-2">
        <div className="mb-1 font-medium">{issue?.title || componentName}</div>
        {issue?.message && <div className="text-xs">{issue?.message}</div>}
      </div>
    </Row>
  );
};

export default MinerStatusRow;
