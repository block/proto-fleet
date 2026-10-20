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
  valid = true,
  presentation = "modal",
  children,
  onSubmit,
  onDismiss,
}: {
  title: string;
  confirm?: string;
  valid?: boolean;
  presentation?: "modal" | "dialog";
  children: ReactNode;
  onSubmit: () => Promise<void>;
  onDismiss: () => void;
}) => {
  const scope = useAccountScope();
  const { run, busy, error, clearError } = useAccountMutation();
  const submit = () => {
    if (valid) void run(onSubmit);
  };
  const buttons: ButtonProps[] = [
    {
      text: "Cancel",
      variant: "secondary",
      onClick: () => {
        clearError();
        onDismiss();
      },
      disabled: busy,
    },
    {
      text: confirm,
      variant: "primary",
      onClick: () => {
        void submit();
      },
      loading: busy,
      disabled: !valid || scope?.readOnly,
    },
  ];
  const content = (
    <div className="flex flex-col gap-4">
      {children}
      {error ? <Callout title={error} intent="danger" prefixIcon={<Alert />} /> : null}
    </div>
  );
  if (presentation === "dialog") {
    return (
      <Dialog open title={title} onDismiss={busy ? undefined : onDismiss} manageFocus buttons={buttons}>
        {content}
      </Dialog>
    );
  }
  return (
    <Modal
      open
      title={title}
      dialogLabel={title}
      onDismiss={busy ? undefined : onDismiss}
      manageFocus
      buttons={buttons.map((button) => ({ ...button, dismissModalOnClick: false }))}
    >
      {content}
    </Modal>
  );
};
