import { useState } from "react";

import { usePoolConfigurationClient } from "./api";
import { ConfigurationReview } from "./ConfigurationReview";
import {
  blankPool,
  canPreservePassword,
  configurationChanges,
  configurationDraft,
  minerWorkerTemplate,
  type PoolDraft,
  poolInput,
} from "./draft";
import { PoolConnectionTest } from "./PoolConnectionTest";
import type {
  Configuration,
  ReviewEditResponse,
} from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";
import { ActionModal } from "@/protoFleet/features/hostedAccounts/components/ActionModal";
import { fleetUsernameHelperText, validateURLScheme } from "@/shared/components/MiningPools/PoolForm/constants";
import { ConfigurationForm } from "@/shared/components/PoolConfigurations/ConfigurationForm";

export const ConfigurationEditor = ({
  original,
  onSaved,
  onDismiss,
}: {
  original?: Configuration;
  onSaved: (configuration: Configuration, operationId: string) => void;
  onDismiss: () => void;
}) => {
  const { client, accountId } = usePoolConfigurationClient();
  const [name, setName] = useState(original?.name ?? "");
  const [pools, setPools] = useState(() => configurationDraft(original));
  const [review, setReview] = useState<ReviewEditResponse>();
  const validPool = (pool: PoolDraft) =>
    !!pool.url &&
    !validateURLScheme(pool.url) &&
    !!pool.username.trim() &&
    (!pool.preservePassword || canPreservePassword(pool, original));
  const valid = !!name.trim() && pools.every(validPool);
  const confirm = async (reviewId: string, current: () => boolean) => {
    const saved = await client.confirmReview({ accountId, reviewId });
    if (!current()) return;
    if (!saved.configuration) throw new Error("Saved configuration was not returned. Refresh and try again.");
    onSaved(saved.configuration, saved.operationId);
    onDismiss();
  };
  if (review)
    return (
      <ConfigurationReview
        review={review}
        editing
        changes={original ? configurationChanges(original, name, pools) : undefined}
        onDismiss={() => setReview(undefined)}
        onApplied={(configuration, operationId) => {
          onSaved(configuration, operationId);
          onDismiss();
        }}
      />
    );
  return (
    <ActionModal
      title={original ? "Edit pool configuration" : "Create pool configuration"}
      confirm={original && original.assignedCount > 0n ? "Review changes" : "Save configuration"}
      valid={valid}
      onDismiss={onDismiss}
      onSubmit={async (current) => {
        const configuration = { name: name.trim(), pools: pools.map(poolInput) };
        if (original) {
          const reviewed = await client.reviewEdit({
            accountId,
            configurationId: original.configurationId,
            expectedVersion: original.version,
            configuration,
          });
          if (!current()) return;
          if (reviewed.affectedMiners || reviewed.affectedHashboards) setReview(reviewed);
          else await confirm(reviewed.reviewId, current);
        } else {
          const created = await client.createConfiguration({ accountId, configuration });
          if (!current()) return;
          if (!created.configuration) throw new Error("Saved configuration was not returned. Refresh and try again.");
          onSaved(created.configuration, "");
          onDismiss();
        }
      }}
    >
      <ConfigurationForm
        name={name}
        onChangeName={setName}
        pools={pools}
        onChangePools={setPools}
        newPool={blankPool}
        validateURL={validateURLScheme}
        hasSavedPassword={(pool) =>
          original?.pools.some((saved) => saved.slotId === pool.slotId && saved.hasPassword) ?? false
        }
        canPreservePassword={(pool) => canPreservePassword(pool, original)}
        usernameHelp={(pool) => {
          if (pool.workerTemplate === minerWorkerTemplate) return fleetUsernameHelperText;
          if (pool.workerTemplate) return "Saved worker naming will be preserved.";
          return "This username is applied as entered.";
        }}
        connectionTest={(pool) => <PoolConnectionTest pool={pool} original={original} valid={validPool(pool)} />}
      />
    </ActionModal>
  );
};
