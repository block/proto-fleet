import { type ReactNode, useRef, useState } from "react";
import { Code, ConnectError } from "@connectrpc/connect";

import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAccountScope } from "@/protoFleet/features/hostedAccounts/AccountScope";
import { useFleetStore } from "@/protoFleet/store";
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
  const [busy, setBusy] = useState(false);
  const submitting = useRef(false);
  const [error, setError] = useState("");
  const scope = useAccountScope();
  const submit = async () => {
    if (submitting.current) return;
    submitting.current = true;
    const generation = useFleetStore.getState().auth.sessionGeneration;
    setBusy(true);
    setError("");
    try {
      await onSubmit();
    } catch (e) {
      setError(getErrorMessage(e));
      if (ConnectError.from(e).code === Code.PermissionDenied) scope?.refreshAccount();
      if (
        ConnectError.from(e).code === Code.Unauthenticated &&
        useFleetStore.getState().auth.sessionGeneration === generation
      )
        useFleetStore.getState().auth.logout();
    } finally {
      submitting.current = false;
      setBusy(false);
    }
  };
  const buttons: ButtonProps[] = [
    { text: "Cancel", variant: "secondary", onClick: onDismiss, disabled: busy },
    {
      text: confirm,
      variant: "primary",
      onClick: () => {
        void submit();
      },
      loading: busy,
      disabled: !valid,
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
