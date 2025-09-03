import { useFirmwareUpdateContext } from "@/protoOS/features/firmwareUpdate/";
import ProgressCircular from "@/shared/components/ProgressCircular";
import StatusOverlay from "@/shared/components/StatusOverlay";

const InstallingOverlay = () => {
  const { updateStatus } = useFirmwareUpdateContext();

  return (
    <StatusOverlay
      text={updateStatus?.message ?? "Installing firmware update"}
      icon={
        <ProgressCircular
          size={32}
          indeterminate
          value={updateStatus?.progress ?? undefined}
        />
      }
    />
  );
};

export default InstallingOverlay;
