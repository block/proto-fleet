import { useCallback, useState } from "react";
import { timestampDate } from "@bufbuild/protobuf/wkt";

import { usePoolConfigurationClient } from "./api";
import { ACCOUNT_READ_TIMEOUT_MS } from "@/protoFleet/features/hostedAccounts/api";
import { PageControls } from "@/protoFleet/features/hostedAccounts/components/PageControls";
import { useAccountQuery } from "@/protoFleet/features/hostedAccounts/useAccountQuery";
import { ConfigurationUpdateRow } from "@/shared/components/PoolConfigurations/ConfigurationUpdateRow";

export const ConfigurationUpdates = ({
  configurationId,
  onOpen,
}: {
  configurationId: string;
  onOpen: (id: string) => void;
}) => {
  const { client, accountId } = usePoolConfigurationClient();
  const [tokens, setTokens] = useState([""]);
  const [page, setPage] = useState(0);
  const load = useCallback(
    (signal: AbortSignal) =>
      client.listOperations(
        { accountId, configurationId, pageToken: tokens[page] },
        { signal, timeoutMs: ACCOUNT_READ_TIMEOUT_MS },
      ),
    [client, accountId, configurationId, page, tokens],
  );
  const query = useAccountQuery(load);
  return (
    <div className="flex flex-col gap-3">
      <h3 className="text-heading-100">Update history</h3>
      {query.loading ? <p>Loading updates…</p> : null}
      {query.error ? <p role="alert">{query.error}</p> : null}
      {query.data?.operations.length === 0 ? <p>No equipment updates yet.</p> : null}
      {query.data?.operations.map((operation) => (
        <ConfigurationUpdateRow
          key={operation.operationId}
          time={operation.createdAt ? timestampDate(operation.createdAt) : undefined}
          summary={
            <>
              {operation.targetCount.toString()} equipment
              {operation.pendingCount > 0n ? ` · ${operation.pendingCount} updating` : ""}
              {operation.failedCount > 0n ? ` · ${operation.failedCount} need attention` : ""}
            </>
          }
          onView={() => onOpen(operation.operationId)}
        />
      ))}
      {query.data ? (
        <PageControls
          previous={page > 0}
          next={!!query.data.nextPageToken}
          onPrevious={() => setPage((p) => p - 1)}
          onNext={() => {
            setTokens((values) => [...values.slice(0, page + 1), query.data!.nextPageToken]);
            setPage((p) => p + 1);
          }}
        />
      ) : null}
    </div>
  );
};
