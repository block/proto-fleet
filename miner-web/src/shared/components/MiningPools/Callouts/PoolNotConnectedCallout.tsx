import { PoolIndex } from "../types";
import { getPoolType } from "../utility";
import { Info } from "@/shared/assets/icons";
import {
  DismissibleCalloutWrapper,
  intents,
} from "@/shared/components/Callout";

interface PoolNotConnectedCalloutProps {
  currentPoolIndex: PoolIndex;
  onDismiss: () => void;
  show: boolean;
}

const PoolNotConnectedCallout = ({
  currentPoolIndex,
  onDismiss,
  show,
}: PoolNotConnectedCalloutProps) => {
  return (
    <DismissibleCalloutWrapper
      icon={<Info />}
      intent={intents.warning}
      onDismiss={onDismiss}
      show={show}
      title={
        <>
          We couldn’t connect with your {getPoolType(currentPoolIndex)} pool.
          <br />
          Review your pool details and try again.
        </>
      }
      testId="pool-not-connected-callout"
    />
  );
};

export default PoolNotConnectedCallout;
