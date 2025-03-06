import MinerStatusRow from "./MinerStatusRow";
import { ErrorListResponse } from "@/protoOS/api/types";

interface MinerStatusRowsProps {
  errors: ErrorListResponse;
}

const MinerStatusRows = ({ errors }: MinerStatusRowsProps) => (
  <>
    {errors.map((error) => (
      <MinerStatusRow error={error} key={error.error_code} />
    ))}
  </>
);

export default MinerStatusRows;
