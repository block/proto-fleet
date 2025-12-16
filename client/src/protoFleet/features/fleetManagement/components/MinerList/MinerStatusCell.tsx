import { useState } from "react";
import MinerStatus from "./MinerStatus";
import { ProtoFleetStatusModal } from "@/protoFleet/components/StatusModal";

type MinerStatusCellProps = {
  deviceIdentifier: string;
  selectedItems?: string[];
};

/**
 * MinerStatusCell wraps the MinerStatus component and handles the modal state.
 * This component manages opening/closing the StatusModal when the status is clicked.
 */
const MinerStatusCell = ({ deviceIdentifier, selectedItems }: MinerStatusCellProps) => {
  const [isModalOpen, setModalOpen] = useState(false);

  const handleStatusClick = () => {
    setModalOpen(true);
  };

  const handleModalClose = () => {
    setModalOpen(false);
  };

  return (
    <>
      <MinerStatus deviceIdentifier={deviceIdentifier} selectedItems={selectedItems} onClick={handleStatusClick} />
      {isModalOpen && (
        <ProtoFleetStatusModal show={isModalOpen} onClose={handleModalClose} deviceId={deviceIdentifier} />
      )}
    </>
  );
};

export default MinerStatusCell;
