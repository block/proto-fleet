import { useEffect, useRef, useState } from "react";
import { ConnectError } from "@connectrpc/connect";
import { awaitEquipmentCommand } from "./awaitEquipmentCommand";
import { powerFeedActions } from "./modulePowerFeed";
import { notQueued, powerFeedFailure, type PowerFeedOutcome, powerFeedRefusal } from "./powerFeedOutcome";
import { containersClient } from "@/protoFleet/api/clients";
import { Power } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";
import {
  type PowerFeedAction,
  powerFeedActionConsequence,
  powerFeedActionLabels,
} from "@/shared/components/PowerFeedCard";

function describe(outcome: PowerFeedOutcome, action: PowerFeedAction): string {
  switch (outcome.status) {
    case "pending":
      return "Sent. Waiting for the controller's result…";
    case "confirmed":
      return action === "cycle"
        ? "The controller accepted the cycle and owns the restore. Refresh the card to follow its recovery cycle until the feed is back on."
        : "The controller accepted the command. The card shows the feed's state from its next reading.";
    case "failed":
      return `Not applied. ${outcome.message}`;
    case "unknown":
      return "Fleet could not confirm the result. The controller may have applied it, so check the feed's state before trying again.";
  }
}

export default function PowerFeedActionDialog({
  moduleId,
  deviceIdentifier,
  feedId,
  containerName,
  tank,
  module,
  action,
  cycleHoldSeconds,
  onClose,
  onSettled,
}: {
  moduleId: bigint;
  /** The controller that was inspected; the server refuses if the module has since moved. */
  deviceIdentifier: string;
  feedId: string;
  containerName: string;
  tank: number;
  module: number;
  action: PowerFeedAction;
  cycleHoldSeconds: number;
  onClose: () => void;
  /** Called once the command reaches any outcome, so the card can re-read the feed. */
  onSettled: () => void;
}) {
  const [sending, setSending] = useState(false);
  const [outcome, setOutcome] = useState<PowerFeedOutcome>();
  const tracking = useRef({ submitted: false, settled: false, onSettled });
  const abort = useRef(new AbortController());
  useEffect(() => {
    tracking.current.onSettled = onSettled;
  }, [onSettled]);
  useEffect(() => {
    const pending = abort.current;
    const command = tracking.current;
    return () => {
      pending.abort();
      // Closed before the result arrived: the card still re-reads the feed.
      if (command.submitted && !command.settled) {
        command.settled = true;
        command.onSettled();
      }
    };
  }, []);
  const label = powerFeedActionLabels[action];

  const settle = (next: PowerFeedOutcome) => {
    if (abort.current.signal.aborted) return;
    tracking.current.settled = true;
    setOutcome(next);
    onSettled();
  };
  const submit = async () => {
    // Send at most once per dialog: a resend could repeat a write that ran.
    if (tracking.current.submitted) return;
    tracking.current.submitted = true;
    setSending(true);
    let command;
    try {
      command = await containersClient.controlModulePowerFeed(
        { moduleId, deviceIdentifier, action: powerFeedActions[action], feedId },
        { timeoutMs: 10_000 },
      );
    } catch (cause) {
      settle(notQueued.has(ConnectError.from(cause).code) ? powerFeedRefusal(cause) : { status: "unknown" });
      setSending(false);
      return;
    }
    setSending(false);
    setOutcome({ status: "pending" });
    try {
      await awaitEquipmentCommand(command.deviceIdentifier, command.batchIdentifier, abort.current.signal);
      settle({ status: "confirmed" });
    } catch (cause) {
      settle(powerFeedFailure(cause));
    }
  };

  // Closing now would stop tracking, and a later result would be shown nowhere.
  const waiting = sending || outcome?.status === "pending";
  const danger = action === "turn_off" || action === "cycle";
  const iconIntent = danger ? "critical" : action === "turn_on" ? "success" : "info";
  return (
    <Dialog
      open
      title={`${label} ${feedId}?`}
      subtitle={`${containerName} · Tank ${tank} · Module ${module}`}
      onDismiss={waiting ? undefined : onClose}
      icon={<DialogIcon intent={iconIntent}>{<Power />}</DialogIcon>}
      testId="power-feed-action-dialog"
      manageFocus
      buttons={
        outcome
          ? [{ text: "Close", variant: variants.primary, onClick: onClose, disabled: waiting }]
          : [
              { text: "Cancel", variant: variants.secondary, onClick: onClose, disabled: sending },
              {
                text: sending ? "Sending…" : label,
                variant: danger ? variants.danger : variants.primary,
                onClick: () => void submit(),
                disabled: sending,
              },
            ]
      }
    >
      <div className="flex flex-col gap-3 text-300 text-text-primary-70">
        {powerFeedActionConsequence(action, cycleHoldSeconds).map((line) => (
          <p key={line}>{line}</p>
        ))}
        {outcome ? (
          <p
            role={outcome.status === "failed" || outcome.status === "unknown" ? "alert" : "status"}
            className={
              outcome.status === "failed" || outcome.status === "unknown"
                ? "text-intent-critical-text"
                : "text-text-primary"
            }
          >
            {describe(outcome, action)}
          </p>
        ) : null}
      </div>
    </Dialog>
  );
}
