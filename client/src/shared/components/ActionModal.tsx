import { type ReactNode } from "react";

import { Alert } from "@/shared/assets/icons";
import { type ButtonProps } from "@/shared/components/ButtonGroup";
import Callout from "@/shared/components/Callout";
import Dialog from "@/shared/components/Dialog";
import Modal from "@/shared/components/Modal";

export const ActionModal = ({
  title,
  busy = false,
  error,
  readOnly = false,
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
  busy?: boolean;
  error?: string;
  readOnly?: boolean;
  confirm?: string;
  allowDismissWhileBusy?: boolean;
  confirmVariant?: ButtonProps["variant"];
  cancel?: string;
  valid?: boolean;
  presentation?: "modal" | "dialog";
  initialFocus?: "firstControl" | "dialog";
  children: ReactNode;
  onSubmit: () => void;
  onDismiss: () => void;
}) => {
  const dismissalLocked = busy && !allowDismissWhileBusy;
  const submit = () => {
    if (valid && !busy && !readOnly) onSubmit();
  };
  const confirmButton: ButtonProps = {
    text: confirm,
    variant: confirmVariant,
    onClick: submit,
    loading: busy,
    disabled: !valid || readOnly,
  };
  const buttons: ButtonProps[] = [
    {
      text: cancel,
      variant: "secondary",
      onClick: () => {
        onDismiss();
      },
      disabled: dismissalLocked,
    },
    confirmButton,
  ];
  const content = (
    <div className="flex flex-col gap-4">
      <fieldset className="flex flex-col gap-4" disabled={busy || readOnly}>
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
