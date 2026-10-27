import { useRequiredAccountScope } from "../AccountScope";
import { PoolConfigurationEditor } from "./PoolConfigurationEditor";
import type { PoolRevision } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";

export const PoolRevisionEditor = ({
  original,
  onSaved,
  onDismiss,
}: {
  original?: PoolRevision;
  onSaved: () => void;
  onDismiss: () => void;
}) => {
  const { account, client } = useRequiredAccountScope();
  return (
    <PoolConfigurationEditor
      original={original}
      onDismiss={onDismiss}
      onSave={async (configuration) => {
        if (original)
          await client.revisePoolConfiguration({
            accountId: account.accountId,
            poolId: original.poolId,
            expectedRevisionId: original.revisionId,
            configuration,
          });
        else await client.createPoolConfiguration({ accountId: account.accountId, configuration });
        onSaved();
      }}
    />
  );
};
