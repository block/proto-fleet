import { type ReactNode } from "react";

import { variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal/Modal";

interface RenameOptionsModalProps {
  children: ReactNode;
  onConfirm: () => void;
  onDismiss: () => void;
  desktopSaveTestId: string;
  mobileSaveTestId: string;
  saveDisabled?: boolean;
}

interface RenameOptionsModalSectionProps {
  children: ReactNode;
}
const buildModalActions = (
  onDismiss: () => void,
  onConfirm: () => void,
  desktopSaveTestId: string,
  mobileSaveTestId: string,
  saveDisabled: boolean,
) => ({
  buttons: [
    {
      text: "Save",
      variant: variants.primary,
      onClick: onConfirm,
      disabled: saveDisabled,
      testId: desktopSaveTestId,
    },
  ],
  phoneFooterButtons: [
    {
      text: "Cancel",
      variant: variants.secondary,
      onClick: onDismiss,
    },
    {
      text: "Save",
      variant: variants.primary,
      onClick: onConfirm,
      disabled: saveDisabled,
      testId: mobileSaveTestId,
    },
  ],
});

const RenameOptionsModal = ({
  children,
  onConfirm,
  onDismiss,
  desktopSaveTestId,
  mobileSaveTestId,
  saveDisabled = false,
}: RenameOptionsModalProps) => (
  <Modal
    open={true}
    title="Options"
    onDismiss={onDismiss}
    hideHeaderOnPhone
    divider={false}
    headerSpacingClassName="mt-4"
    phoneSheet
    {...buildModalActions(onDismiss, onConfirm, desktopSaveTestId, mobileSaveTestId, saveDisabled)}
  >
    {children}
  </Modal>
);

export const RenameOptionsModalBody = ({ children }: RenameOptionsModalSectionProps) => {
  return <div className="mt-6 flex flex-col gap-6 lg:mt-10">{children}</div>;
};

export const RenameOptionsModalPreview = ({ children }: RenameOptionsModalSectionProps) => {
  return <div className="max-w-[592px]">{children}</div>;
};

export default RenameOptionsModal;
