import { DismissibleCalloutWrapper, intents } from "components/Callout";

import { Success } from "icons";

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
      subtitle="The mining pool connection was successful."
      testId="pool-connected-callout"
    />
  );
};

export default PoolConnectedCallout;
