import { type ReactNode } from "react";

import { useAccountMutation } from "../useAccountMutation";
import { useAccountScope } from "@/protoFleet/features/hostedAccounts/AccountScope";
import { ActionModal as SharedActionModal } from "@/shared/components/ActionModal";
import { type ButtonProps } from "@/shared/components/ButtonGroup";

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
  return (
    <SharedActionModal
      title={title}
      confirm={confirm}
      allowDismissWhileBusy={allowDismissWhileBusy}
      confirmVariant={confirmVariant}
      cancel={cancel}
      valid={valid}
      presentation={presentation}
      initialFocus={initialFocus}
      busy={busy}
      error={error}
      readOnly={scope?.readOnly}
      onSubmit={() => void run(onSubmit)}
      onDismiss={() => {
        clearError();
        onDismiss();
      }}
    >
      {children}
    </SharedActionModal>
  );
};
