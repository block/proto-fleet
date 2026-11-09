import { type ReactNode } from "react";

import { useAccountMutation } from "../useAccountMutation";
import { useAccountScope } from "@/protoFleet/features/hostedAccounts/AccountScope";
import { Alert } from "@/shared/assets/icons";
import { type ButtonProps } from "@/shared/components/ButtonGroup";
import Callout from "@/shared/components/Callout";
import Dialog from "@/shared/components/Dialog";
import Modal from "@/shared/components/Modal";

export const ActionModal = ({
  title,
  confirm = "Save",
  allowDismissWhileBusy = false,
  confirmVariant = "primary",
  cancel = "Cancel",
  valid = true,
  presentation = "modal",
  initialFocus,
  children,
  onSubmit,
  onDismiss,
}: {
  title: string;
  confirm?: string;
  allowDismissWhileBusy?: boolean;
  confirmVariant?: ButtonProps["variant"];
  cancel?: string;
  valid?: boolean;
  presentation?: "modal" | "dialog";
  initialFocus?: "firstControl" | "dialog";
  children: ReactNode;
  onSubmit: (current: () => boolean) => Promise<void>;
  onDismiss: () => void;
}) => {
  const scope = useAccountScope();
  const { run, busy, error, clearError } = useAccountMutation();
  const dismissalLocked = busy && !allowDismissWhileBusy;
  const submit = () => {
    if (valid) void run(onSubmit);
  };
  const confirmButton: ButtonProps = {
    text: confirm,
    variant: confirmVariant,
    onClick: submit,
    loading: busy,
    disabled: !valid || scope?.readOnly,
  };
  const buttons: ButtonProps[] = [
    {
      text: cancel,
      variant: "secondary",
      onClick: () => {
        clearError();
        onDismiss();
      },
      disabled: dismissalLocked,
    },
    confirmButton,
  ];
  const content = (
    <div className="flex flex-col gap-4">
      <fieldset className="flex flex-col gap-4" disabled={busy || scope?.readOnly}>
        {children}
      </fieldset>
      {error ? <Callout title={error} intent="danger" prefixIcon={<Alert />} /> : null}
    </div>
  );
  if (presentation === "dialog") {
    return (
      <Dialog open title={title} onDismiss={dismissalLocked ? undefined : onDismiss} manageFocus buttons={buttons}>
        {content}
      </Dialog>
    );
  }
  return (
    <Modal
      open
      title={title}
      dialogLabel={title}
      onDismiss={dismissalLocked ? undefined : onDismiss}
      manageFocus
      initialFocus={initialFocus}
      buttons={buttons.map((button) => ({ ...button, dismissModalOnClick: false }))}
    >
      {content}
    </Modal>
  );
};
