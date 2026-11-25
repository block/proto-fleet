import { useMemo } from "react";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { withAuthRetry } from "@/protoOS/features/controllerLogs/controllerRecordsSource";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import BoardHistory from "@/shared/features/boardHistory/BoardHistory";
import type { BoardHistorySource } from "@/shared/features/boardHistory/types";

/**
 * Event history and recovery evidence for one CT1 hashboard, read from the
 * controller by MAC. Renders nothing where the adapter has no board history,
 * which includes ProtoOS hosted by Fleet.
 */
const BoardHistorySection = ({ mac }: { mac: string }) => {
  const { adapter } = useMinerHosting();
  const authRetry = useAuthRetry();
  const source = useMemo((): BoardHistorySource | undefined => {
    const { getBoardEvents, listRecoveryEvidence, getRecoveryEvidence } = adapter ?? {};
    if (!adapter || !getBoardEvents) return undefined;
    const withAuth = withAuthRetry(authRetry);
    return {
      readEvents: withAuth((query, options) => getBoardEvents.call(adapter, mac, query, options)),
      listEvidence:
        listRecoveryEvidence && withAuth((_: void, options) => listRecoveryEvidence.call(adapter, mac, options)),
      readEvidence: getRecoveryEvidence && withAuth(getRecoveryEvidence.bind(adapter)),
    };
  }, [adapter, authRetry, mac]);
  // Keyed by board: another board starts from its own live window, and a read
  // still in flight for the previous board cannot land under this one.
  return source ? <BoardHistory key={mac} {...source} boardLabel={mac} /> : null;
};

export default BoardHistorySection;
