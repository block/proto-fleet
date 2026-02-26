import MinerStatus from "./MinerStatus";

type MinerStatusCellProps = {
  deviceIdentifier: string;
  selectedItems?: string[];
  onOpenStatusFlow: (deviceIdentifier: string) => void;
};

const MinerStatusCell = ({ deviceIdentifier, selectedItems, onOpenStatusFlow }: MinerStatusCellProps) => {
  return (
    <MinerStatus
      deviceIdentifier={deviceIdentifier}
      selectedItems={selectedItems}
      onClick={() => onOpenStatusFlow(deviceIdentifier)}
    />
  );
};

export default MinerStatusCell;
