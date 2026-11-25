import { useCallback, useEffect, useRef, useState } from "react";
import { Code, ConnectError } from "@connectrpc/connect";

import { usePoolConfigurationClient } from "./api";
import {
  type ConfirmationAttempt,
  type ConfirmationDetails,
  forgetConfirmation,
  markConfirmationUnknown,
  startConfirmationAttempt,
  useConfirmationRecovery,
} from "./confirmationAttempts";
import { FleetErrorDetailsSchema } from "@/protoFleet/api/generated/common/v1/common_pb";
import { ConfirmReviewErrorCode } from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";
import { useAccountScope } from "@/protoFleet/features/hostedAccounts/AccountScope";
import { ACCOUNT_READ_TIMEOUT_MS } from "@/protoFleet/features/hostedAccounts/api";
import { useFleetStore } from "@/protoFleet/store";

export class ConfirmationError extends ConnectError {
  constructor(
    cause: unknown,
    public readonly uncertain: boolean,
    public readonly expiredUnaccepted = false,
  ) {
    const error = ConnectError.from(cause);
    super(
      uncertain ? "Fleet couldn’t confirm whether the update was accepted. It may still be running." : error.rawMessage,
      error.code,
    );
  }
}

// Each mounted review belongs to its initiating session and account lifetime.
// Ending that lifetime preserves attempted IDs but never continues the batch.
export const useReviewedConfirmation = () => {
  const { client, accountId } = usePoolConfigurationClient();
  const scope = useAccountScope();
  const generation = useFleetStore((state) => state.auth.sessionGeneration);
  const [origin] = useState(() => ({
    generation,
    username: useFleetStore.getState().auth.username,
    accountId,
    signal: scope?.signal,
  }));
  const active = useRef(true);
  const pending = useRef(new Map<string, ConfirmationAttempt>());
  const close = useCallback(() => {
    active.current = false;
    pending.current.forEach(markConfirmationUnknown);
    pending.current.clear();
  }, []);
  useEffect(() => {
    active.current =
      generation === origin.generation && accountId === origin.accountId && scope?.signal === origin.signal;
    scope?.signal.addEventListener("abort", close);
    return () => {
      scope?.signal.removeEventListener("abort", close);
      close();
    };
  }, [generation, accountId, origin, scope?.signal, close]);
  const current = () =>
    active.current &&
    scope?.signal === origin.signal &&
    !origin.signal?.aborted &&
    useFleetStore.getState().auth.sessionGeneration === origin.generation &&
    useFleetStore.getState().auth.username === origin.username &&
    accountId === origin.accountId;
  const confirm = async (details: ConfirmationDetails, password: string) => {
    if (!current()) return;
    if (
      useConfirmationRecovery
        .getState()
        .attempts.some(
          (a) =>
            a.username === origin.username &&
            a.accountId === accountId &&
            a.reviewId === details.reviewId &&
            a.status === "submitting",
        )
    )
      throw new ConfirmationError(new ConnectError("Another confirmation is already in progress.", Code.Aborted), true);
    const { attempt, wasUnknown } = startConfirmationAttempt(origin.username, accountId, details);
    pending.current.set(attempt.id, attempt);
    try {
      const saved = await client.confirmReview(
        { accountId, reviewId: details.reviewId, password },
        { timeoutMs: ACCOUNT_READ_TIMEOUT_MS },
      );
      if (!current()) return;
      if (!saved.configuration || (details.kind === "worker_names" && !saved.operationId))
        throw new Error("Confirmation result is incomplete.");
      forgetConfirmation(attempt);
      return { ...saved, configuration: saved.configuration };
    } catch (cause) {
      if (!current()) return;
      // A rejection of a later retry cannot disprove acceptance of an earlier
      // request whose reply was lost (for example, permissions changed).
      const error = ConnectError.from(cause);
      const expiredUnaccepted =
        error.code === Code.FailedPrecondition &&
        error
          .findDetails(FleetErrorDetailsSchema)
          .some(
            (detail) =>
              detail.code.case === "endpoint" && detail.code.value === ConfirmReviewErrorCode.EXPIRED_UNACCEPTED,
          );
      const uncertain =
        !expiredUnaccepted &&
        (wasUnknown ||
          [Code.Unknown, Code.Unavailable, Code.DeadlineExceeded, Code.Canceled, Code.Internal, Code.DataLoss].includes(
            error.code,
          ));
      if (uncertain) markConfirmationUnknown(attempt);
      else forgetConfirmation(attempt);
      throw new ConfirmationError(cause, uncertain, expiredUnaccepted);
    } finally {
      if (!current()) markConfirmationUnknown(attempt);
      pending.current.delete(attempt.id);
    }
  };
  return { confirm, current, close };
};
