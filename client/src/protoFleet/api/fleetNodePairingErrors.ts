import { Code, ConnectError } from "@connectrpc/connect";
import { getErrorCause } from "@/protoFleet/api/requestErrors";

// These pre-dispatch errors leave every submitted miner safe to retry. Keep
// them aligned with the Fleet Node pair handler and control dispatcher.
const NO_CONTROL_STREAM = "fleet node has no active control stream";
const UNCONFIRMED_NODE = "fleet node is not CONFIRMED";
const BUSY_COMMAND_PREFIX = "fleet node is busy with another command; retry shortly:";

export const isDefinitivePairRejection = (error: unknown): boolean => {
  const cause = getErrorCause(error);
  return (
    cause instanceof ConnectError &&
    (cause.code === Code.InvalidArgument ||
      (cause.code === Code.ResourceExhausted && cause.rawMessage.startsWith(BUSY_COMMAND_PREFIX)) ||
      (cause.code === Code.FailedPrecondition && [NO_CONTROL_STREAM, UNCONFIRMED_NODE].includes(cause.rawMessage)))
  );
};
