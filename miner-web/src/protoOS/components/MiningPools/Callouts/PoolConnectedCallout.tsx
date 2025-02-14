import { Success } from "@/shared/assets/icons";
import {
  DismissibleCalloutWrapper,
  intents,
} from "@/shared/components/Callout";

interface PoolConnectedCalloutProps {
  onDismiss: () => void;
  show: boolean;
}

const PoolConnectedCallout = ({
  onDismiss,
  show,
}: PoolConnectedCalloutProps) => {
  return (
    <DismissibleCalloutWrapper
      icon={<Success />}
      intent={intents.success}
      onDismiss={onDismiss}
      show={show}
      title="The mining pool connection was successful."
      testId="pool-connected-callout"
    />
  );
};

export default PoolConnectedCallout;
