import { useFirmwareUpdate } from "@/protoOS/features/firmwareUpdate/";
import ProgressCircular from "@/shared/components/ProgressCircular";
import StatusOverlay from "@/shared/components/StatusOverlay";

const InstallingOverlay = () => {
  const { message, progress } = useFirmwareUpdate();

  return (
    <StatusOverlay
      text={message ?? "Installing firmware update"}
      icon={
        <ProgressCircular
          size={32}
          indeterminate
          value={progress ?? undefined}
        />
      }
    />
  );
};

export default InstallingOverlay;
