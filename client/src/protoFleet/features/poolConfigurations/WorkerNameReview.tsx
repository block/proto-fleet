import { useState } from "react";

import { usePoolConfigurationClient } from "./api";
import { trackPoolOperation } from "./assignmentProgress";
import { ConfirmationError, useReviewedConfirmation } from "./useReviewedConfirmation";
import type { ReviewWorkerNamesResponse } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { useAccountScope } from "@/protoFleet/features/hostedAccounts/AccountScope";
import { ACCOUNT_READ_TIMEOUT_MS } from "@/protoFleet/features/hostedAccounts/api";
import { ActionModal } from "@/protoFleet/features/hostedAccounts/components/ActionModal";

export type WorkerNameUpdateResult = {
  updatedCount: number;
  unchangedCount: number;
  failedCount: number;
  unknownCount?: number;
  notSubmittedCount?: number;
};

export const WorkerNameReview = ({
  review,
  password,
  onDone,
  onCancel,
}: {
  review: ReviewWorkerNamesResponse;
  password: string;
  onDone: (result: WorkerNameUpdateResult) => void;
  onCancel: () => void;
}) => {
  const { client, accountId } = usePoolConfigurationClient();
  const confirmation = useReviewedConfirmation();
  const scope = useAccountScope();
  type Outcome = "submitted" | "unknown" | "failed";
  const [outcomes, setOutcomes] = useState<Record<string, Outcome>>({});
  const changed = review.reviews.flatMap((item) => item.changes);
  const attempted = Object.keys(outcomes).length > 0;
  const finish = (completed: Record<string, Outcome>) => {
    if (!confirmation.current()) return;
    const count = (outcome?: Outcome) =>
      review.reviews
        .filter((item) => completed[item.reviewId] === outcome)
        .reduce((n, item) => n + item.changes.length, 0);
    const unknownCount = count("unknown");
    const notSubmittedCount = count();
    onDone({
      updatedCount: count("submitted"),
      unchangedCount: review.unchangedCount,
      failedCount: review.failedCount + count("failed"),
      ...(unknownCount ? { unknownCount } : {}),
      ...(notSubmittedCount ? { notSubmittedCount } : {}),
    });
    confirmation.close();
  };
  if (!confirmation.current()) return null;
  return (
    <ActionModal
      title="Review worker names"
      confirm={attempted ? "Retry confirmation" : "Save and update equipment"}
      cancel={attempted ? "Close" : "Cancel"}
      presentation="dialog"
      valid={!!password}
      onDismiss={() => {
        if (!confirmation.current()) return;
        if (attempted) finish(outcomes);
        else {
          confirmation.close();
          onCancel();
        }
      }}
      onSubmit={async () => {
        const completed = { ...outcomes };
        // After an interrupted batch, an explicit retry resolves only attempted
        // groups. Untouched groups require a new review and confirmation.
        const items = review.reviews.filter((item) => !attempted || completed[item.reviewId]);
        if (!confirmation.current()) return;
        if (!attempted) {
          await client.authorizeWorkerNameReviews(
            { reviewIds: items.map((item) => item.reviewId), password },
            { signal: scope?.signal, timeoutMs: ACCOUNT_READ_TIMEOUT_MS },
          );
        }
        for (const item of items) {
          if (!confirmation.current()) return;
          if (completed[item.reviewId] === "submitted") continue;
          try {
            const saved = await confirmation.confirm(
              {
                reviewId: item.reviewId,
                configurationId: item.configurationId,
                name: item.configurationName,
                kind: "worker_names",
                deviceIdentifiers: item.changes.map((change) => change.deviceIdentifier),
              },
              "",
            );
            if (!saved || !confirmation.current()) return;
            completed[item.reviewId] = "submitted";
            setOutcomes({ ...completed });
            void trackPoolOperation(client, accountId, saved.configuration, saved.operationId, [], scope?.signal);
          } catch (error) {
            if (!confirmation.current()) return;
            completed[item.reviewId] = error instanceof ConfirmationError && !error.uncertain ? "failed" : "unknown";
            setOutcomes({ ...completed });
            throw error;
          }
        }
        finish(completed);
      }}
    >
      <p>
        Save {changed.length} worker names and reapply each miner’s current pool configuration. Pool changes may briefly
        interrupt mining.
      </p>
      <div className="max-h-72 overflow-auto" data-dd-privacy="mask">
        {review.reviews.map((item) => (
          <section key={item.reviewId} className="my-4">
            <h3 className="font-medium">
              {item.configurationName}
              {outcomes[item.reviewId] === "submitted"
                ? " — update submitted"
                : outcomes[item.reviewId] === "unknown"
                  ? " — confirmation unknown"
                  : ""}
            </h3>
            <ul>
              {item.changes.map((change) => (
                <li key={change.deviceIdentifier}>
                  {change.deviceIdentifier}: {change.before || "Unnamed"} → {change.after}
                </li>
              ))}
            </ul>
          </section>
        ))}
      </div>
      {review.failedCount > 0 ? (
        <p role="status">{review.failedCount} worker names could not be generated and will stay unchanged.</p>
      ) : null}
      {attempted ? (
        <p role="status">
          Closing does not cancel submitted updates. Retry resolves attempted configurations only; changes not yet
          submitted require a new review.
        </p>
      ) : null}
    </ActionModal>
  );
};
