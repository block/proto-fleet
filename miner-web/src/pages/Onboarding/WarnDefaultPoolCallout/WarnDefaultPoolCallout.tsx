import clsx from "clsx";

import { DismissibleCalloutWrapper, intents } from "components/Callout";

import { Info } from "icons";

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
      icon={<Info />}
      intent={intents.danger}
      onDismiss={onDismiss}
      show={show}
      title="A default pool is required to set up your miner."
      testId="warn-default-pool-callout"
    />
  );
};

export default WarnDefaultPoolCallout;
