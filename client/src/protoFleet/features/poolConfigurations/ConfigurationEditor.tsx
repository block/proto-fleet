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
  poolSlotLabel,
} from "./draft";
import { PoolConnectionTest } from "./PoolConnectionTest";
import type {
  Configuration,
  ReviewEditResponse,
} from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";
import { ActionModal } from "@/protoFleet/features/hostedAccounts/components/ActionModal";
import Button from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import Input from "@/shared/components/Input";
import { fleetUsernameHelperText, validateURLScheme } from "@/shared/components/MiningPools/PoolForm/constants";

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
  const update = (key: string, patch: Partial<PoolDraft>) =>
    setPools((items) => items.map((pool) => (pool.key === key ? { ...pool, ...patch } : pool)));
  const move = (index: number, by: number) =>
    setPools((items) => {
      const reordered = [...items];
      [reordered[index], reordered[index + by]] = [reordered[index + by], reordered[index]];
      return reordered;
    });
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
      <Input id="pool-configuration-name" label="Name" initValue={name} onChange={setName} maxLength={255} />
      {pools.map((pool, i) => (
        <div
          key={pool.key}
          role="group"
          aria-label={poolSlotLabel(i)}
          className="flex flex-col gap-4 rounded-xl border border-border-10 p-4"
          data-dd-privacy="mask"
        >
          <div className="flex flex-wrap items-center justify-between gap-2">
            <h3 className="text-heading-100">{poolSlotLabel(i)}</h3>
            <div className="flex gap-3">
              {i > 0 ? <Button variant="textOnly" text="Move up" onClick={() => move(i, -1)} /> : null}
              {i < pools.length - 1 ? <Button variant="textOnly" text="Move down" onClick={() => move(i, 1)} /> : null}
              {pools.length > 1 ? (
                <Button
                  variant="textOnly"
                  text="Remove"
                  onClick={() => setPools((items) => items.filter((item) => item.key !== pool.key))}
                />
              ) : null}
            </div>
          </div>
          <Input
            id={`pool-url-${pool.key}`}
            label="Pool URL"
            initValue={pool.url}
            sanitize={(value) => value.trim()}
            onChange={(url) => update(pool.key, { url })}
            error={validateURLScheme(pool.url)}
            maxLength={256}
          />
          <Input
            id={`pool-username-${pool.key}`}
            label="Username"
            initValue={pool.username}
            onChange={(username) => update(pool.key, { username })}
            maxLength={255}
          />
          <p className="text-200 text-text-primary-50">
            {pool.workerTemplate === minerWorkerTemplate
              ? fleetUsernameHelperText
              : pool.workerTemplate
                ? "Saved worker naming will be preserved."
                : "This username is applied as entered."}
          </p>
          {original?.pools.some((saved) => saved.slotId === pool.slotId && saved.hasPassword) ? (
            <label className="flex items-center gap-3">
              <Checkbox
                checked={pool.preservePassword}
                onChange={(event) => update(pool.key, { preservePassword: event.target.checked })}
              />
              Keep saved password
            </label>
          ) : null}
          {pool.preservePassword ? (
            <p className="text-200 text-text-primary-50">
              {canPreservePassword(pool, original)
                ? "The saved password stays hidden."
                : "The URL or username changed. Uncheck Keep saved password to replace or clear it."}
            </p>
          ) : (
            <Input
              id={`pool-password-${pool.key}`}
              label="Pool password (optional)"
              type="password"
              initValue={pool.password}
              onChange={(password) => update(pool.key, { password })}
              autoComplete="new-password"
              maxLength={255}
            />
          )}
          <PoolConnectionTest pool={pool} original={original} valid={validPool(pool)} />
        </div>
      ))}
      {pools.length < 3 ? (
        <Button
          variant="secondary"
          text="Add backup pool"
          className="self-start"
          onClick={() => setPools((items) => [...items, blankPool()])}
        />
      ) : null}
    </ActionModal>
  );
};
