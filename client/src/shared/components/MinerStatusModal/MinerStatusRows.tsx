import { ReactNode } from "react";
import MinerStatusRow from "./MinerStatusRow";
import { type Issue } from "./types";

interface MinerStatusRowsProps {
  issues?: Issue[];
  icon?: ReactNode;
  componentName: string;
  disabled?: boolean;
}

const MinerStatusRows = ({
  issues = [],
  icon,
  componentName,
  disabled = false,
}: MinerStatusRowsProps) => (
  <>
    {issues.length === 0 || disabled ? (
      <MinerStatusRow
        icon={icon}
        componentName={componentName}
        disabled={disabled}
      />
    ) : (
      <>
        {issues.map((issue, idx) => (
          <MinerStatusRow
            issue={issue}
            key={`${componentName.replace(" ", "_")}_${idx}`}
            icon={icon}
            componentName={componentName}
          />
        ))}
      </>
    )}
  </>
);

export default MinerStatusRows;
