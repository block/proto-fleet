import MinerStatus from "./MinerStatus";
import { useMinerStatus } from "@/protoOS/contexts/MinerStatusContext/useMinerStatus";
import { type ButtonVariant } from "@/shared/components/Button";

interface MinerStatusWrapperProps {
  variant?: ButtonVariant;
}

const MinerStatusWrapper = ({ variant }: MinerStatusWrapperProps) => {
  const { comprehensiveStatus } = useMinerStatus();

  return <MinerStatus status={comprehensiveStatus} variant={variant} />;
};

export default MinerStatusWrapper;
