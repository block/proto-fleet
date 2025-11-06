import type { ComponentError } from "./types";
import { Alert } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Divider from "@/shared/components/Divider";
import Row from "@/shared/components/Row";

interface ComponentErrorRowProps {
  error: ComponentError;
  divider: boolean;
}

const ComponentErrorRow = ({ error, divider }: ComponentErrorRowProps) => {
  const formatTimestamp = (timestamp?: number) => {
    if (!timestamp) return "";
    const date = new Date(timestamp * 1000);
    return `${date.toLocaleDateString(undefined, {
      month: "numeric",
      day: "numeric",
      year: "2-digit",
    })} at ${date.toLocaleTimeString(undefined, {
      hour: "numeric",
      minute: "2-digit",
      hour12: true,
    })}`;
  };

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
