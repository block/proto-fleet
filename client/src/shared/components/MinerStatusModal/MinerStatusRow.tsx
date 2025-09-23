import { ReactNode } from "react";
import { clsx } from "clsx";

import ProtoRigStatus from "./ProtoRigStatus";
import { type Issue } from "./types";
import { iconSizes } from "@/shared/assets/icons/constants";
import Row from "@/shared/components/Row";
import StatusCircle, { statuses } from "@/shared/components/StatusCircle";

interface MinerStatusRowProps {
  issue?: Issue;
  icon?: ReactNode;
  componentName: string;
  disabled?: boolean;
  isProtoRig?: boolean;
}

const MinerStatusRow = ({
  issue,
  icon,
  componentName,
  disabled = false,
  isProtoRig,
}: MinerStatusRowProps) => {
  return (
    <Row
      prefixIcon={
        icon && isProtoRig ? (
          <ProtoRigStatus
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
