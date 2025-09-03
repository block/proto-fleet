import { useContext } from "react";
import FirmwareUpdateContext from "./FirmwareUpdateContext";

const useFirmwareUpdateContext = () => {
  const { updateStatus, pending, dismissed, setDismissed, installing } =
    useContext(FirmwareUpdateContext);

  return {
    updateStatus,
    pending,
    dismissed,
    setDismissed,
    installing,
  };
};

export default useFirmwareUpdateContext;
