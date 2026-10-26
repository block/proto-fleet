import { useState } from "react";

import { blankSlot, canPreservePassword, poolSlotInput, type SlotDraft, validWorkerTemplate } from "../poolDraft";
import { ActionModal } from "./ActionModal";
import type { PoolRevision, PoolRevisionInput } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import Button from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import Input from "@/shared/components/Input";
import { validateURLScheme } from "@/shared/components/MiningPools/PoolForm/constants";

export const PoolConfigurationEditor = ({
  original,
  onSave,
  fixedWorker = false,
  onDismiss,
}: {
  original?: PoolRevision;
  onSave: (configuration: {
    name: string;
    slots: Omit<PoolRevisionInput["slots"][number], "$typeName">[];
  }) => Promise<void>;
  fixedWorker?: boolean;
  onDismiss: () => void;
}) => {
  const [name, setName] = useState(original?.name ?? "");
  const [slots, setSlots] = useState<SlotDraft[]>(
    original
      ? original.slots.map((s) => ({
          url: s.url,
          username: s.username,
          workerTemplate: s.workerTemplate,
          password: "",
          preservePassword: s.hasPassword,
        }))
      : [blankSlot()],
  );
  const update = (priority: number, patch: Partial<SlotDraft>) =>
    setSlots((items) => items.map((slot, i) => (i === priority ? { ...slot, ...patch } : slot)));
  const valid =
    !!name.trim() &&
    slots.every(
      (slot, i) =>
        !!slot.url.trim() &&
        !validateURLScheme(slot.url) &&
        (!fixedWorker || slot.url.startsWith("stratum+tcp://")) &&
        !!slot.username.trim() &&
        validWorkerTemplate(slot.workerTemplate) &&
        (!slot.preservePassword || canPreservePassword(slot, original, i)),
    );
  return (
    <ActionModal
      title={original ? "Edit pool configuration" : "Create pool configuration"}
      confirm={original ? "Save new revision" : "Create configuration"}
      valid={valid}
      onDismiss={onDismiss}
      onSubmit={async () => {
        const configuration = { name: name.trim(), slots: slots.map(poolSlotInput) };
        await onSave(configuration);
        onDismiss();
      }}
    >
      <Input id="hosted-pool-name" label="Name" initValue={name} onChange={setName} maxLength={255} />
      {slots.map((slot, i) => (
        <div
          key={i}
          role="group"
          aria-label={i === 0 ? "Primary pool" : `Backup ${i}`}
          className="flex flex-col gap-4 rounded-xl border border-border-10 p-4"
          data-dd-privacy="mask"
        >
          <div className="flex items-center justify-between text-heading-100 text-text-primary">
            <span>{i === 0 ? "Primary pool" : `Backup ${i}`}</span>
            {i > 0 ? (
              <Button
                variant="textOnly"
                text="Remove backup"
                onClick={() => setSlots((items) => items.filter((_, priority) => i !== priority))}
              />
            ) : null}
          </div>
          <Input
            id={`hosted-pool-url-${i}`}
            label="Pool URL"
            initValue={slot.url}
            onChange={(url) => update(i, { url })}
            error={
              fixedWorker && slot.url && !slot.url.startsWith("stratum+tcp://")
                ? "Use stratum+tcp:// for mining groups."
                : validateURLScheme(slot.url)
            }
            maxLength={256}
          />
          <Input
            id={`hosted-pool-username-${i}`}
            label={fixedWorker ? "Worker username" : "Payout username"}
            initValue={slot.username}
            onChange={(username) => update(i, { username })}
            maxLength={255}
          />
          {!fixedWorker ? (
            <>
              <Input
                id={`hosted-pool-worker-${i}`}
                label="Worker suffix (optional)"
                initValue={slot.workerTemplate}
                onChange={(workerTemplate) => update(i, { workerTemplate })}
                error={
                  !validWorkerTemplate(slot.workerTemplate)
                    ? "Use only {miner_id} or {miner_name} placeholders."
                    : undefined
                }
                maxLength={128}
              />
              <p className="text-200 text-text-primary-50">
                {slot.username || "payout"}
                {slot.workerTemplate
                  ? `.${slot.workerTemplate.replace("{miner_id}", "123").replace("{miner_name}", "Miner-123")}`
                  : ""}
              </p>
            </>
          ) : null}
          {original?.slots[i]?.hasPassword || slot.preservePassword ? (
            <label className="flex items-center gap-3">
              <Checkbox
                checked={slot.preservePassword}
                onChange={(e) => update(i, { preservePassword: e.target.checked })}
              />
              Keep saved password
            </label>
          ) : null}
          {slot.preservePassword ? (
            <p className="text-200 text-text-primary-50">
              {canPreservePassword(slot, original, i)
                ? "The saved password stays hidden."
                : "The URL or username changed. Uncheck Keep saved password to replace or clear it."}
            </p>
          ) : (
            <>
              <Input
                id={`hosted-pool-password-${i}`}
                label="Pool password (optional)"
                type="password"
                initValue={slot.password}
                onChange={(password) => update(i, { password })}
                autoComplete="new-password"
                maxLength={255}
              />
              <p className="text-200 text-text-primary-50">Leave blank for no password.</p>
            </>
          )}
        </div>
      ))}
      {slots.length < 3 ? (
        <Button
          variant="secondary"
          text="Add backup pool"
          onClick={() => setSlots((items) => [...items, blankSlot()])}
        />
      ) : null}
      {original ? (
        <p className="text-200 text-text-primary-50">
          Saving creates a revision. Existing miner pools and the incoming selection stay unchanged.
        </p>
      ) : null}
    </ActionModal>
  );
};
