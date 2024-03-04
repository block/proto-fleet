import clsx from "clsx";

import { DismissibleCalloutWrapper, intents } from "components/Callout";

import InfoIcon from "icons/Info";

interface WarnDefaultPoolCalloutProps {
  onDismiss: () => void;
  show: boolean;
}

const WarnDefaultPoolCallout = ({
  onDismiss,
  show,
}: WarnDefaultPoolCalloutProps) => {
  return (
    <DismissibleCalloutWrapper
      className={clsx({ "!mb-10": show })}
      icon={<InfoIcon />}
      intent={intents.danger}
      onDismiss={onDismiss}
      show={show}
      subtitle="A default pool is required to set up your ProtoMiner."
      testId="warn-default-pool-callout"
    />
  );
};

export default WarnDefaultPoolCallout;
