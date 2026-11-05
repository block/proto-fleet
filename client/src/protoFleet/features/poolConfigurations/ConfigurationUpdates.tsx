import { useCallback, useState } from "react";
import { timestampDate } from "@bufbuild/protobuf/wkt";

import { usePoolConfigurationClient } from "./api";
import { ACCOUNT_READ_TIMEOUT_MS } from "@/protoFleet/features/hostedAccounts/api";
import { PageControls } from "@/protoFleet/features/hostedAccounts/components/PageControls";
import { useAccountQuery } from "@/protoFleet/features/hostedAccounts/useAccountQuery";
import Button from "@/shared/components/Button";

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
        <div key={operation.operationId} className="flex items-center justify-between gap-4">
          <div>
            <p>{operation.createdAt ? timestampDate(operation.createdAt).toLocaleString() : "Pool update"}</p>
            <p className="text-200 text-text-primary-50">
              {operation.targetCount.toString()} equipment
              {operation.pendingCount > 0n ? ` · ${operation.pendingCount} updating` : ""}
              {operation.failedCount > 0n ? ` · ${operation.failedCount} need attention` : ""}
            </p>
          </div>
          <Button variant="textOnly" text="View results" onClick={() => onOpen(operation.operationId)} />
        </div>
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
