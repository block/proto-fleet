import {
  ComponentStatus,
  type MinerComponentStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import StatusCircle, { statuses } from "@/shared/components/StatusCircle";

type MinerStatusProps = {
  isSelected?: boolean;
  status: MinerComponentStatus;
};

// maps ComponentStatus to the status that StatusCircle uses
const statusMap = {
  [ComponentStatus.UNSPECIFIED]: statuses.inactive,
  [ComponentStatus.OK]: statuses.normal,
  [ComponentStatus.WARNING]: statuses.warning,
  [ComponentStatus.ERROR]: statuses.error,
  [ComponentStatus.OFFLINE]: statuses.inactive,
  [ComponentStatus.PENDING]: statuses.pending,
};

const MinerStatus = ({ isSelected = false, status }: MinerStatusProps) => {
  return (
    <div className="flex flex-row opacity-70">
      <StatusCircle
        status={statusMap[status.hashBoards]}
        variant="simple"
        width="w-[6px]"
        isSelected={isSelected}
      />
      <StatusCircle
        status={statusMap[status.psu]}
        variant="simple"
        width="w-[6px]"
        isSelected={isSelected}
      />
      <StatusCircle
        status={statusMap[status.fans]}
        variant="simple"
        width="w-[6px]"
        isSelected={isSelected}
      />
      <StatusCircle
        status={statusMap[status.controlBoard]}
        variant="simple"
        width="w-[6px]"
        isSelected={isSelected}
      />
    </div>
  );
};

export default MinerStatus;
