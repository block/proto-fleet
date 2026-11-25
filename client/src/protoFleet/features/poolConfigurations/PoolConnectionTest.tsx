import { useState } from "react";

import { usePoolConfigurationClient, usePoolConfigurationPermissions } from "./api";
import { type PoolDraft, poolInput } from "./draft";
import type { Configuration } from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";
import { Alert, Success } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Button from "@/shared/components/Button";
import { DismissibleCalloutWrapper, intents } from "@/shared/components/Callout";

export const PoolConnectionTest = ({
  pool,
  original,
  valid,
}: {
  pool: PoolDraft;
  original?: Configuration;
  valid: boolean;
}) => {
  const { client, accountId, readOnly } = usePoolConfigurationClient();
  const { manage } = usePoolConfigurationPermissions();
  const [test, setTest] = useState<{
    pool: PoolDraft;
    accountId: string;
    pending: boolean;
    message: string;
    connected?: boolean;
  }>();
  const current = test?.pool === pool && test.accountId === accountId ? test : undefined;
  return (
    <div className="flex flex-col gap-2">
      <Button
        variant="secondary"
        text="Test connection"
        className="self-start"
        loading={current?.pending}
        disabled={!valid || current?.pending || readOnly || !manage}
        onClick={async () => {
          const request = { pool, accountId, pending: true, message: "" };
          setTest(request);
          let message: string;
          let connected = false;
          try {
            const result = await client.testConnection({
              accountId,
              configurationId: original?.configurationId ?? "",
              expectedVersion: original?.version ?? 0n,
              pool: poolInput(pool),
            });
            connected = result.connected;
            message = connected
              ? "Pool connection successful"
              : "We couldn't connect with your pool. Review your pool details and try again.";
          } catch {
            message = "Connection test unavailable. Refresh and try again.";
          }
          // An older response must never replace a newer slot's test result.
          setTest((latest) => (latest === request ? { ...request, pending: false, message, connected } : latest));
        }}
      />
      <div role="status">
        {current && !current.pending ? (
          <DismissibleCalloutWrapper
            icon={current.connected ? <Success /> : <Alert width={iconSizes.medium} />}
            intent={current.connected ? intents.success : intents.danger}
            onDismiss={() => setTest(undefined)}
            show
            title={current.message}
          />
        ) : null}
      </div>
    </div>
  );
};
