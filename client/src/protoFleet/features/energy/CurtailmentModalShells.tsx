import type { ReactElement } from "react";
import { CurtailmentForm } from "./CurtailmentForm";
import type { CurtailmentModalShell, CurtailmentStartModalProps } from "./curtailmentModalTypes";
const createShell: CurtailmentModalShell = {
  mode: "create",
  variant: "curtailment",
  isEditMode: false,
  isResponseProfileVariant: false,
  isResponseProfileEditMode: false,
  isLiveCurtailmentEditMode: false,
  shouldResetResponseProfileOnEdit: true,
  requiresRunConfirmation: true,
  nameFieldId: "curtailment-reason",
  nameFieldLabel: "Reason",
  modalTitle: "New curtailment",
  closeAriaLabel: "Close curtailment planner",
  primaryButtonText: "Run curtailment",
  shouldShowResponseProfileSelector: true,
};
const editShell: CurtailmentModalShell = {
  ...createShell,
  mode: "edit",
  isEditMode: true,
  isLiveCurtailmentEditMode: true,
  shouldResetResponseProfileOnEdit: false,
  requiresRunConfirmation: false,
  modalTitle: "Manage curtailment",
  closeAriaLabel: "Close curtailment editor",
  primaryButtonText: "Save",
  shouldShowResponseProfileSelector: false,
};
export function CreateCurtailmentModal(props: CurtailmentStartModalProps): ReactElement {
  return (
    <CurtailmentForm
      {...props}
      shell={createShell}
      onStopCurtailment={undefined}
      onTestCurtailment={undefined}
      onDeleteResponseProfile={undefined}
    />
  );
}
export function EditCurtailmentModal(props: CurtailmentStartModalProps): ReactElement {
  return (
    <CurtailmentForm
      {...props}
      shell={editShell}
      responseProfiles={undefined}
      onTestCurtailment={undefined}
      onDeleteResponseProfile={undefined}
    />
  );
}
export function ResponseProfileModal(props: CurtailmentStartModalProps): ReactElement {
  const editing = props.responseProfileMode === "edit";
  const shell: CurtailmentModalShell = {
    ...createShell,
    mode: props.mode ?? "create",
    variant: "responseProfile",
    isEditMode: props.mode === "edit",
    isResponseProfileVariant: true,
    isResponseProfileEditMode: editing,
    shouldResetResponseProfileOnEdit: false,
    requiresRunConfirmation: false,
    nameFieldId: "response-profile-name",
    nameFieldLabel: "Profile name",
    modalTitle: editing ? "Edit response profile" : "Create response profile",
    closeAriaLabel: editing ? "Close response profile editor" : "Close response profile creator",
    primaryButtonText: "Save profile",
    shouldShowResponseProfileSelector: false,
  };
  return <CurtailmentForm {...props} shell={shell} onStopCurtailment={undefined} responseProfiles={undefined} />;
}
