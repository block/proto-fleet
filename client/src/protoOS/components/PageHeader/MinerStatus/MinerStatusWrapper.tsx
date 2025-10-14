import MinerStatus from "./MinerStatus";
import { useComprehensiveStatus } from "@/protoOS/store";
import { type ButtonVariant } from "@/shared/components/Button";

interface MinerStatusWrapperProps {
  variant?: ButtonVariant;
}

const MinerStatusWrapper = ({ variant }: MinerStatusWrapperProps) => {
  const comprehensiveStatus = useComprehensiveStatus();

  return <MinerStatus status={comprehensiveStatus} variant={variant} />;
};

export default MinerStatusWrapper;
