import { ErrorListResponse } from "apiTypes";

import MinerStatusRow from "./MinerStatusRow";

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
