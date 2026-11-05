import { useState } from "react";

import { usePoolConfigurationClient } from "./api";
import { trackPoolOperation } from "./assignmentProgress";
import { minerWorkerTemplate, poolSlotLabel } from "./draft";
import { useReviewedConfirmation } from "./useReviewedConfirmation";
import type {
  Configuration,
  ReviewedEquipment,
} from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";
import { useAccountScope } from "@/protoFleet/features/hostedAccounts/AccountScope";
import { ActionModal } from "@/protoFleet/features/hostedAccounts/components/ActionModal";
import Button from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import { fleetUsernameHelperText } from "@/shared/components/MiningPools/PoolForm/constants";

export type ConfigurationChange = { field: string; before: string; after: string };
export type ConfigurationReviewData = {
  reviewId: string;
  configuration?: Configuration;
  affectedMiners: bigint;
  affectedHashboards: bigint;
  equipment?: ReviewedEquipment[];
};

export const ConfigurationReview = ({
  review,
  changes,
  editing = false,
  initialPassword = "",
  onApplied,
  onDismiss,
}: {
  review: ConfigurationReviewData;
  changes?: ConfigurationChange[];
  editing?: boolean;
  initialPassword?: string;
  onApplied: (configuration: Configuration, operationId: string) => void;
  onDismiss: () => void;
}) => {
  const { client, accountId } = usePoolConfigurationClient();
  const scope = useAccountScope();
  const confirmation = useReviewedConfirmation();
  const [attempted, setAttempted] = useState(false);
  const [password, setPassword] = useState(initialPassword);
  const [visibleEquipment, setVisibleEquipment] = useState(25);
  if (!confirmation.current()) return null;
  return (
    <ActionModal
      title={editing ? "Review pool configuration changes" : "Confirm pool assignment"}
      confirm={attempted ? "Retry confirmation" : editing ? "Save and update equipment" : "Assign"}
      cancel={attempted ? "Close" : "Cancel"}
      presentation="dialog"
      valid={!!password}
      onDismiss={() => {
        confirmation.close();
        onDismiss();
      }}
      onSubmit={async () => {
        if (!confirmation.current()) return;
        setAttempted(true);
        const saved = await confirmation.confirm(
          {
            reviewId: review.reviewId,
            configurationId: review.configuration?.configurationId ?? "",
            name: review.configuration?.name ?? "Pool configuration",
            kind: "pool_configuration",
            deviceIdentifiers: (review.equipment ?? [])
              .filter((item) => item.kind === "device")
              .map((item) => item.deviceIdentifier),
          },
          password,
        );
        if (!saved || !confirmation.current()) return;
        setPassword("");
        if (!saved.configuration) throw new Error("Saved configuration was not returned. Refresh and try again.");
        if (saved.operationId)
          void trackPoolOperation(
            client,
            accountId,
            saved.configuration,
            saved.operationId,
            review.equipment ?? [],
            scope?.signal,
          );
        onApplied(saved.configuration, saved.operationId);
      }}
    >
      <p>
        {editing
          ? `Saving this change will update ${review.affectedMiners} miners with this pool configuration applied.`
          : `Apply ${review.configuration?.name ?? "this configuration"} to ${review.affectedMiners} miners?`}
      </p>
      {review.affectedHashboards > 0n ? (
        <p>This will also update {review.affectedHashboards.toString()} hashboards.</p>
      ) : null}
      <p>Pool changes may briefly interrupt mining.</p>
      {review.equipment?.length ? (
        <section aria-label="Affected equipment">
          <h3 className="font-medium">Affected equipment ({review.equipment.length})</h3>
          <ul className="mt-3 max-h-48 list-inside list-disc overflow-y-auto" data-dd-privacy="mask">
            {review.equipment.slice(0, visibleEquipment).map((equipment) => (
              <li key={`${equipment.kind}:${equipment.targetId}`}>
                {equipment.label || `${equipment.kind === "device" ? "Miner" : "Hashboard"} ${equipment.targetId}`}
              </li>
            ))}
          </ul>
          {visibleEquipment < review.equipment.length ? (
            <Button
              variant="textOnly"
              text="Show more equipment"
              onClick={() => setVisibleEquipment((count) => count + 25)}
            />
          ) : null}
        </section>
      ) : null}
      <div className="flex flex-col gap-3" data-dd-privacy="mask">
        {changes?.length ? (
          <dl className="flex flex-col gap-3">
            {changes.map((change) => (
              <div key={change.field}>
                <dt className="font-medium">{change.field}</dt>
                <dd className="break-all">
                  <span className="text-text-primary-50">{change.before || "None"}</span> → {change.after || "None"}
                </dd>
              </div>
            ))}
          </dl>
        ) : (
          editing &&
          review.configuration?.pools.map((pool, i) => (
            <div key={pool.slotId} className="break-all">
              <strong>{poolSlotLabel(i)}</strong>
              <p>{pool.url}</p>
              <p>
                {pool.username}
                {pool.workerTemplate && pool.workerTemplate !== minerWorkerTemplate ? `.${pool.workerTemplate}` : ""}
              </p>
              {pool.workerTemplate === minerWorkerTemplate ? (
                <p className="text-200 text-text-primary-50">{fleetUsernameHelperText}</p>
              ) : null}
            </div>
          ))
        )}
        <Input
          id="pool-configuration-confirm-password"
          label="Your password"
          type="password"
          autoComplete="current-password"
          initValue={password}
          onChange={setPassword}
          maxLength={1024}
        />
      </div>
    </ActionModal>
  );
};
