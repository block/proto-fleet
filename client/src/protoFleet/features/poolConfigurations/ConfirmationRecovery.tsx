import { useEffect, useState } from "react";

import { usePoolConfigurationClient, usePoolConfigurationPermissions } from "./api";
import { trackPoolOperation } from "./assignmentProgress";
import { type ConfirmationAttempt, useConfirmationRecovery } from "./confirmationAttempts";
import { ConfirmationError, useReviewedConfirmation } from "./useReviewedConfirmation";
import { useAccountScope } from "@/protoFleet/features/hostedAccounts/AccountScope";
import { ActionModal } from "@/protoFleet/features/hostedAccounts/components/ActionModal";
import { useFleetStore } from "@/protoFleet/store";
import Dialog from "@/shared/components/Dialog";
import Input from "@/shared/components/Input";
import { pushToast, removeToast, STATUSES } from "@/shared/features/toaster";

// The existing persistent-toast surface makes recovery reachable after the
// originating dialog closes, after navigation, and after a same-tab reload.
export const ConfirmationRecovery = () => {
  const { accountId, readOnly } = usePoolConfigurationClient();
  const { change } = usePoolConfigurationPermissions();
  const auth = useFleetStore((state) => state.auth);
  const attempts = useConfirmationRecovery((state) => state.attempts);
  const [selected, setSelected] = useState<{ attempt: ConfirmationAttempt; generation: number }>();
  const visible = auth.isAuthenticated && change && !readOnly;
  // Recreate the toast when an attempt becomes uncertain again; clear it as
  // soon as its owner/account/session stops being visible.
  useEffect(() => {
    if (!visible) return;
    const toasts = attempts
      .filter((a) => a.username === auth.username && a.accountId === accountId && a.status === "unknown")
      .map((attempt) =>
        pushToast({
          message: `${attempt.kind === "worker_names" ? "Worker name update" : "Pool configuration update"}: confirmation unknown (${attempt.name}).`,
          status: STATUSES.error,
          longRunning: true,
          ttl: false,
          actions: [
            {
              label: "Review update",
              onClick: () => {
                const current = useFleetStore.getState().auth;
                if (
                  current.sessionGeneration === auth.sessionGeneration &&
                  current.username === auth.username &&
                  current.isAuthenticated
                )
                  setSelected({ attempt, generation: auth.sessionGeneration });
              },
            },
          ],
        }),
      );
    return () => toasts.forEach((id) => removeToast(id));
  }, [attempts, accountId, auth.username, auth.sessionGeneration, visible]);
  const attempt =
    visible &&
    selected?.generation === auth.sessionGeneration &&
    selected.attempt.username === auth.username &&
    selected.attempt.accountId === accountId
      ? selected.attempt
      : undefined;
  return attempt ? (
    <RecoverConfirmation
      key={`${attempt.id}:${auth.sessionGeneration}`}
      attempt={attempt}
      onDismiss={() => setSelected(undefined)}
    />
  ) : null;
};

const RecoverConfirmation = ({ attempt, onDismiss }: { attempt: ConfirmationAttempt; onDismiss: () => void }) => {
  const { client, accountId } = usePoolConfigurationClient();
  const confirmation = useReviewedConfirmation();
  const scope = useAccountScope();
  const [password, setPassword] = useState("");
  const [expiredUnaccepted, setExpiredUnaccepted] = useState(false);
  if (expiredUnaccepted)
    return (
      <Dialog
        open
        title="Update not submitted"
        onDismiss={onDismiss}
        manageFocus
        buttons={[{ text: "Close", variant: "primary", onClick: onDismiss }]}
      >
        <p>This review expired without being accepted. Review your changes again to submit a new update.</p>
      </Dialog>
    );
  return (
    <ActionModal
      title="Resolve update confirmation"
      confirm="Retry confirmation"
      cancel="Close"
      presentation="dialog"
      valid={!!password}
      onDismiss={() => {
        confirmation.close();
        onDismiss();
      }}
      onSubmit={async () => {
        let saved;
        try {
          saved = await confirmation.confirm(attempt, password);
        } catch (error) {
          if (error instanceof ConfirmationError && error.expiredUnaccepted) {
            setPassword("");
            setExpiredUnaccepted(true);
            return;
          }
          throw error;
        }
        if (!saved || !confirmation.current()) return;
        if (saved.operationId)
          void trackPoolOperation(client, accountId, saved.configuration, saved.operationId, [], scope?.signal);
        onDismiss();
      }}
    >
      <p>Fleet couldn’t confirm whether the update to {attempt.name} was accepted. It may still be running.</p>
      <p>
        Retry returns the existing update if it was accepted, or submits the original change if it is still valid. It
        does not submit other configurations.
      </p>
      {attempt.deviceIdentifiers.length ? <p>{attempt.deviceIdentifiers.length} miners included.</p> : null}
      <Input
        id="recover-confirmation-password"
        label="Your password"
        type="password"
        autoComplete="current-password"
        initValue={password}
        onChange={setPassword}
        maxLength={1024}
      />
    </ActionModal>
  );
};
