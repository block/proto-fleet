import { ReactNode } from "react";
import MinerStatusRow from "./MinerStatusRow";
import { type Issue } from "./types";

interface MinerStatusRowsProps {
  issues?: Issue[];
  icon?: ReactNode;
  componentName: string;
  disabled?: boolean;
  isProtoRig?: boolean;
}

const MinerStatusRows = ({
  issues = [],
  icon,
  componentName,
  disabled = false,
  isProtoRig,
}: MinerStatusRowsProps) => (
  <>
    {issues.length === 0 || disabled ? (
      <MinerStatusRow
        icon={icon}
        componentName={componentName}
        disabled={disabled}
        isProtoRig={isProtoRig}
      />
    ) : (
      <>
        {issues.map((issue, idx) => (
          <MinerStatusRow
            issue={issue}
            key={`${componentName.replace(" ", "_")}_${idx}`}
            icon={icon}
            componentName={componentName}
            isProtoRig={isProtoRig}
          />
        ))}
      </>
    )}
  </>
);

export default MinerStatusRows;
