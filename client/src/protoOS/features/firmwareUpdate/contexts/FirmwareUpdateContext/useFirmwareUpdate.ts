import { useContext } from "react";
import FirmwareUpdateContext from "./FirmwareUpdateContext";

const useFirmwareUpdate = () => {
  const {
    status,
    pending,
    version,
    changelog,
    message,
    progress,
    dismissed,
    setDismissed,
    updateFirmware,
    installing,
  } = useContext(FirmwareUpdateContext);

  return {
    status,
    pending,
    version,
    changelog,
    message,
    dismissed,
    progress,
    setDismissed,
    updateFirmware,
    installing,
  };
};

export default useFirmwareUpdate;
