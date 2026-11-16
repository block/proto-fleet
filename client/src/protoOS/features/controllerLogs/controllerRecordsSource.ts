import type { AdapterRequestOptions, MinerAdapter } from "@/protoOS/api/adapters/types";
import type { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import type { ControllerRecordsRead, ControllerRecordsSource } from "@/shared/features/controllerLogs/types";

type AdapterRead<Query, Data> = (query: Query, options: AdapterRequestOptions) => Promise<Data>;

/** Adapts the miner adapter's reads to the shared source; each refreshes its token once on a 401. */
export const controllerRecordsSource = (
  adapter: MinerAdapter,
  authRetry: ReturnType<typeof useAuthRetry>,
): ControllerRecordsSource => {
  const withAuth =
    <Query, Data>(request: AdapterRead<Query, Data>): ControllerRecordsRead<Query, Data> =>
    (query, signal) =>
      new Promise<Data>((resolve, reject) => {
        void authRetry({ signal, request: (options) => request(query, options), onSuccess: resolve, onError: reject })
          // An aborted read settles without either callback. Reject whatever
          // happened so a read never stays pending and stops polling; after
          // resolve or reject this is a no-op.
          .then(() =>
            reject(signal.aborted ? signal.reason : new Error("The controller read ended without a result.")),
          );
      });
  return {
    readLogs: adapter.getControllerLogs && withAuth(adapter.getControllerLogs.bind(adapter)),
    listAuditEvents: adapter.listAuditEvents && withAuth(adapter.listAuditEvents.bind(adapter)),
  };
};
