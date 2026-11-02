import { type ReactElement, useCallback, useState } from "react";
import {
  adminTerminateReasonRequiredMessage,
  type ForceReleaseCurtailmentOptions,
  type AdminTerminateCurtailmentOptions as TerminateRecoveryOptions,
  type AdminTerminateCurtailmentState as TerminateRecoveryState,
} from "@/protoFleet/api/useCurtailmentApi";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";
import Radio from "@/shared/components/Radio";
import Textarea from "@/shared/components/Textarea";

interface TerminateRecoveryDialogProps {
  error?: string | null;
  isSubmitting?: boolean;
  onCancel: () => void;
  onConfirm: (options: TerminateRecoveryOptions) => void;
  open: boolean;
}

interface ForceReleaseDialogProps {
  error?: string | null;
  isSubmitting?: boolean;
  mode: "curtailment" | "restore";
  onCancel: () => void;
  onConfirm: (options: ForceReleaseCurtailmentOptions) => void;
  open: boolean;
}

const terminateRecoveryStateOptions: { label: string; value: TerminateRecoveryState }[] = [
  { label: "Cancelled", value: "cancelled" },
  { label: "Failed", value: "failed" },
];

export function CurtailmentRecoveryTerminateDialog({
  error,
  isSubmitting = false,
  onCancel,
  onConfirm,
  open,
}: TerminateRecoveryDialogProps): ReactElement {
  const [targetState, setTargetState] = useState<TerminateRecoveryState>("cancelled");
  const [reason, setReason] = useState("");
  const [reasonError, setReasonError] = useState<string | null>(null);
  const validationError = reasonError ?? error ?? null;

  const confirmTerminate = useCallback(() => {
    const trimmedReason = reason.trim();
    if (!trimmedReason) {
      setReasonError(adminTerminateReasonRequiredMessage);
      return;
    }

    setReasonError(null);
    onConfirm({ reason: trimmedReason, targetState });
  }, [onConfirm, reason, targetState]);
  const dismissDialog = isSubmitting ? undefined : onCancel;

  return (
    <Dialog
      open={open}
      title="Stop restore?"
      onDismiss={dismissDialog}
      icon={
        <DialogIcon intent="critical">
          <Alert />
        </DialogIcon>
      }
      buttons={[
        {
          text: "Cancel",
          variant: variants.secondary,
          onClick: onCancel,
          disabled: isSubmitting,
        },
        {
          text: "Stop restore",
          variant: variants.danger,
          onClick: confirmTerminate,
          loading: isSubmitting,
        },
      ]}
    >
      <div className="grid gap-4 text-300 text-text-primary">
        <p className="text-text-primary-70">
          Use this after restore has started. This closes the event audit trail as cancelled or failed.
        </p>
        <fieldset className="grid gap-2">
          <legend className="text-emphasis-300">Target state</legend>
          <div className="flex flex-wrap gap-4">
            {terminateRecoveryStateOptions.map((option) => (
              <label key={option.value} className="flex items-center gap-2">
                <Radio
                  name="terminate-recovery-target-state"
                  value={option.value}
                  selected={targetState === option.value}
                  onChange={() => setTargetState(option.value)}
                  disabled={isSubmitting}
                />
                <span>{option.label}</span>
              </label>
            ))}
          </div>
        </fieldset>
        <Textarea
          id="terminate-recovery-reason"
          label="Reason"
          initValue={reason}
          rows={3}
          maxLength={256}
          required
          error={validationError ?? false}
          onChange={(value) => {
            setReason(value);
            if (value.trim()) {
              setReasonError(null);
            }
          }}
        />
      </div>
    </Dialog>
  );
}

export function CurtailmentForceReleaseDialog({
  error,
  isSubmitting = false,
  mode,
  onCancel,
  onConfirm,
  open,
}: ForceReleaseDialogProps): ReactElement {
  const [reason, setReason] = useState("");
  const [reasonError, setReasonError] = useState<string | null>(null);
  const validationError = reasonError ?? error ?? null;
  const title = mode === "restore" ? "Abort restore?" : "Abort curtailment?";
  const confirmText = mode === "restore" ? "Abort restore" : "Abort curtailment";
  const body =
    mode === "restore"
      ? "This aborts the restore workflow by immediately releasing curtailment ownership. If automation owns this event, Abort also disables the automation rule. It attempts to turn facility fans back on, but it does not wake miners or confirm that restore completed."
      : "This cancels the current automation-owned curtailment event and disables the owning automation rule so it cannot immediately curtail miners again. It attempts to turn facility fans back on, but it does not wake miners.";

  const confirmRelease = useCallback(() => {
    const trimmedReason = reason.trim();
    if (!trimmedReason) {
      setReasonError(adminTerminateReasonRequiredMessage);
      return;
    }
    setReasonError(null);
    onConfirm({ reason: trimmedReason });
  }, [onConfirm, reason]);
  const dismissDialog = isSubmitting ? undefined : onCancel;

  return (
    <Dialog
      open={open}
      title={title}
      onDismiss={dismissDialog}
      icon={
        <DialogIcon intent="critical">
          <Alert />
        </DialogIcon>
      }
      buttons={[
        {
          text: "Cancel",
          variant: variants.secondary,
          onClick: onCancel,
          disabled: isSubmitting,
        },
        {
          text: confirmText,
          variant: variants.danger,
          onClick: confirmRelease,
          loading: isSubmitting,
        },
      ]}
    >
      <div className="grid gap-4 text-300 text-text-primary">
        <p className="text-text-primary-70">{body}</p>
        <Textarea
          id="force-release-reason"
          label="Reason"
          initValue={reason}
          rows={3}
          maxLength={256}
          required
          error={validationError ?? false}
          onChange={(value) => {
            setReason(value);
            if (value.trim()) {
              setReasonError(null);
            }
          }}
        />
      </div>
    </Dialog>
  );
}
