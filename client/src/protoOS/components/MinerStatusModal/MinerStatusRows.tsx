import { ReactNode } from "react";
import MinerStatusRow from "./MinerStatusRow";
import { ErrorListResponse } from "@/protoOS/api/types";

interface MinerStatusRowsProps {
  errors: ErrorListResponse;
  icon?: ReactNode;
}

const MinerStatusRows = ({ errors, icon }: MinerStatusRowsProps) => (
  <>
    {errors.map((error, idx) => (
      <MinerStatusRow
        error={error}
        key={`${error.error_code}_${idx}`}
        icon={icon}
      />
    ))}
  </>
);

export default MinerStatusRows;
