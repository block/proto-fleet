import { ComponentStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { useMinerStatus } from "@/protoFleet/features/fleetManagement/store/useFleetStore";
import StatusCircle, { statuses } from "@/shared/components/StatusCircle";

type MinerStatusProps = {
  deviceIdentifier: string;
  selectedItems?: string[];
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

const MinerStatus = ({ deviceIdentifier, selectedItems }: MinerStatusProps) => {
  const statusFromStore = useMinerStatus(deviceIdentifier || "");
  const status = statusFromStore || {
    $typeName: "fleetmanagement.v1.MinerComponentStatus",
    hashBoards: ComponentStatus.UNSPECIFIED,
    controlBoard: ComponentStatus.UNSPECIFIED,
    fans: ComponentStatus.UNSPECIFIED,
    psu: ComponentStatus.UNSPECIFIED,
  };

  const isSelected = selectedItems?.includes(deviceIdentifier);
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
