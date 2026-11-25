import { useState } from "react";
import type { AssignmentPool, LocalPoolConfiguration, SavedPoolSet } from "@/protoOS/api/adapters/poolAssignment";
import { validateContainerPoolURL, validateMiningGroupPoolURL } from "@/protoOS/api/adapters/poolURL";
import { newUUID } from "@/protoOS/utils/uuid";
import { ActionModal } from "@/shared/components/ActionModal";
import Checkbox from "@/shared/components/Checkbox";
import { ConfigurationForm } from "@/shared/components/PoolConfigurations/ConfigurationForm";
import { type ConfigurationPoolDraft, poolSlotLabel } from "@/shared/components/PoolConfigurations/types";

interface PoolDraft extends ConfigurationPoolDraft {
  id?: string;
  enabled?: boolean;
}
const blankPool = (): PoolDraft => ({
  key: newUUID(),
  url: "",
  username: "",
  password: "",
  preservePassword: false,
});
export default function ConfigurationEditor({
  original,
  usernameSource,
  draft,
  maxPools,
  busy,
  error,
  onSubmit,
  onDismiss,
}: {
  original?: LocalPoolConfiguration;
  usernameSource?: SavedPoolSet;
  draft?: { name: string; pools: AssignmentPool[] };
  maxPools: number;
  onSubmit: (name: string, pools: AssignmentPool[]) => void;
  busy?: boolean;
  error?: string;
  onDismiss: () => void;
}) {
  const [name, setName] = useState(draft?.name ?? original?.name ?? "");
  const [pools, setPools] = useState<PoolDraft[]>(() => {
    if (usernameSource && !draft)
      return usernameSource.pools.map(({ url, username }) => ({ ...blankPool(), url, username }));
    const initial = draft?.pools ?? original?.pools;
    return initial?.length
      ? initial.map((pool) => ({
          ...blankPool(),
          ...pool,
          key: pool.id ?? newUUID(),
          password: "password" in pool ? pool.password : "",
          preservePassword: "preservePassword" in pool ? pool.preservePassword === true : true,
        }))
      : [blankPool()];
  });
  const savedPool = (pool: PoolDraft) => original?.pools.find((saved) => saved.id === pool.key);
  const maxAllowedPools = original?.isDefault ? Math.max(maxPools, original.pools.length) : maxPools;
  const validateURL = original?.id === "default" ? validateContainerPoolURL : validateMiningGroupPoolURL;
  const canPreservePassword = (pool: PoolDraft) => {
    const saved = savedPool(pool);
    return saved?.url === pool.url && saved.username === pool.username;
  };
  const valid =
    !!name.trim() &&
    pools.length > 0 &&
    pools.length <= maxAllowedPools &&
    (!usernameSource || pools.some((pool, index) => pool.username !== usernameSource.pools[index].username)) &&
    pools.every(
      (pool) =>
        !!pool.url &&
        !validateURL(pool.url) &&
        !!pool.username.trim() &&
        (!pool.preservePassword || canPreservePassword(pool)) &&
        (original?.id !== "default" || !savedPool(pool) || pool.preservePassword || Boolean(pool.password)),
    );
  return (
    <ActionModal
      title={
        usernameSource ? "Edit pool usernames" : original ? "Edit pool configuration" : "Create pool configuration"
      }
      confirm={
        usernameSource
          ? "Review assignment"
          : original && original.assignedCount > 0
            ? "Review changes"
            : "Save configuration"
      }
      busy={busy}
      error={error}
      valid={valid}
      onDismiss={onDismiss}
      onSubmit={() =>
        onSubmit(
          name.trim(),
          pools.map((pool, index) => ({
            id: pool.id,
            preservePassword: pool.preservePassword,
            enabled: pool.enabled,
            name: poolSlotLabel(index),
            url: pool.url,
            username: pool.username,
            password: pool.password,
          })),
        )
      }
    >
      {usernameSource ? (
        <p>
          This creates a configuration for the selected hashboards. Change at least one complete username. Enter the
          pool password again if your pool requires one.
        </p>
      ) : null}
      <ConfigurationForm
        name={name}
        nameReadOnly={original?.id === "default"}
        onChangeName={setName}
        pools={pools}
        onChangePools={setPools}
        newPool={blankPool}
        maxPools={maxAllowedPools}
        fixedPoolList={Boolean(usernameSource)}
        hasSavedPassword={(pool) => Boolean(savedPool(pool))}
        canPreservePassword={canPreservePassword}
        passwordRequired={(pool) => original?.id === "default" && Boolean(savedPool(pool))}
        validateURL={validateURL}
        poolControls={
          original?.id === "default"
            ? (pool) => (
                <label className="flex items-center gap-3">
                  <Checkbox
                    checked={pool.enabled ?? true}
                    onChange={(event) => {
                      const enabled = event.target.checked;
                      setPools((current) =>
                        current.map((entry) => (entry.key === pool.key ? { ...entry, enabled } : entry)),
                      );
                    }}
                  />
                  Enabled
                </label>
              )
            : undefined
        }
      />
    </ActionModal>
  );
}
