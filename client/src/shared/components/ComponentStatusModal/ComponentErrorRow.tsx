import type { ComponentError } from "./types";
import { Alert } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Divider from "@/shared/components/Divider";
import Row from "@/shared/components/Row";
import { formatTimestamp } from "@/shared/utils/formatTimestamp";

interface ComponentErrorRowProps {
  error: ComponentError;
  divider: boolean;
}

const ComponentErrorRow = ({ error, divider }: ComponentErrorRowProps) => {
  return (
    <>
      <Row
        prefixIcon={
          <div className="flex h-6 w-6 items-center justify-center rounded bg-core-primary-5">
            <Alert className="text-text-critical" width={iconSizes.small} />
          </div>
        }
        divider={false}
      >
        <div className="flex flex-col">
          <div className="text-emphasis-300 font-medium text-text-primary">
            {error.message}
          </div>
          <div className="text-200 text-text-primary-50">
            {formatTimestamp(error.timestamp)}
          </div>
        </div>
      </Row>
      {divider && <Divider />}
    </>
  );
};

export default ComponentErrorRow;
