import { useState } from "react";
import type { AssignmentPool, LocalPoolConfiguration } from "@/protoOS/api/adapters/poolAssignment";
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
  draft,
  maxPools,
  busy,
  error,
  onSubmit,
  onDismiss,
}: {
  original?: LocalPoolConfiguration;
  draft?: { name: string; pools: AssignmentPool[] };
  maxPools: number;
  onSubmit: (name: string, pools: AssignmentPool[]) => void;
  busy?: boolean;
  error?: string;
  onDismiss: () => void;
}) {
  const [name, setName] = useState(draft?.name ?? original?.name ?? "");
  const [pools, setPools] = useState<PoolDraft[]>(() => {
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
  const validateURL = original?.id === "default" ? validateContainerPoolURL : validateMiningGroupPoolURL;
  const canPreservePassword = (pool: PoolDraft) => {
    const saved = savedPool(pool);
    return saved?.url === pool.url && saved.username === pool.username;
  };
  const valid =
    !!name.trim() &&
    pools.length > 0 &&
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
      title={original ? "Edit pool configuration" : "Create pool configuration"}
      confirm={original && original.assignedCount > 0 ? "Review changes" : "Save configuration"}
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
      <ConfigurationForm
        name={name}
        nameReadOnly={original?.id === "default"}
        onChangeName={setName}
        pools={pools}
        onChangePools={setPools}
        newPool={blankPool}
        maxPools={Math.max(Math.min(3, maxPools), original?.pools.length ?? 0)}
        hasSavedPassword={(pool) => Boolean(savedPool(pool))}
        canPreservePassword={canPreservePassword}
        passwordRequired={(pool) => original?.id === "default" && Boolean(savedPool(pool))}
        validateURL={validateURL}
        usernameHelp={() => "This username is applied as entered."}
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
