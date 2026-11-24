import { type Dispatch, type KeyboardEvent, type SetStateAction, useCallback, useState } from "react";

import { AutomationResponseProfileRevisionConflictError } from "@/protoFleet/api/automationResponseProfileRevisionConflict";
import { isInputEnterSaveEvent } from "@/protoFleet/features/settings/components/Curtailment/keyboard";
import { variants } from "@/shared/components/Button";

type ResponseProfileBinding = {
  responseProfileId: string;
  responseProfileRevision: string;
};

type AutomationRuleModalActionsOptions<Values extends ResponseProfileBinding> = {
  mode: "create" | "edit";
  // Completes "Failed to save …." and "Failed to delete …."
  ruleNoun: string;
  values: Values;
  setValues: Dispatch<SetStateAction<Values>>;
  setShowValidationErrors: Dispatch<SetStateAction<boolean>>;
  hasValidationErrors: boolean;
  isSaveUnavailable: boolean;
  saving: boolean;
  deleting: boolean;
  onSave: (values: Values) => Promise<void>;
  onDelete?: () => Promise<void>;
  onDismiss: () => void;
};

/**
 * Save, delete, and Enter-to-save handling shared by the automation rule
 * modals. A revision conflict adopts the latest response profile revision so
 * the next save can succeed.
 */
export function useAutomationRuleModalActions<Values extends ResponseProfileBinding>({
  mode,
  ruleNoun,
  values,
  setValues,
  setShowValidationErrors,
  hasValidationErrors,
  isSaveUnavailable,
  saving,
  deleting,
  onSave,
  onDelete,
  onDismiss,
}: AutomationRuleModalActionsOptions<Values>) {
  const [saveError, setSaveError] = useState<string | null>(null);
  const isBusy = saving || deleting;

  const handleSave = useCallback(async () => {
    if (isSaveUnavailable) {
      return;
    }
    if (hasValidationErrors) {
      setShowValidationErrors(true);
      return;
    }

    try {
      setSaveError(null);
      await onSave(values);
      onDismiss();
    } catch (error) {
      if (error instanceof AutomationResponseProfileRevisionConflictError) {
        const latestRevision = error.latestResponseProfileRevisionById.get(values.responseProfileId) ?? "";
        setValues((currentValues) => ({ ...currentValues, responseProfileRevision: latestRevision }));
      }
      setSaveError(error instanceof Error && error.message ? error.message : `Failed to save ${ruleNoun}.`);
    }
  }, [hasValidationErrors, isSaveUnavailable, onDismiss, onSave, ruleNoun, setShowValidationErrors, setValues, values]);

  const handleFormKeyDown = useCallback(
    (event: KeyboardEvent<HTMLDivElement>) => {
      if (!isInputEnterSaveEvent(event)) {
        return;
      }
      event.preventDefault();
      void handleSave();
    },
    [handleSave],
  );

  const handleDelete = useCallback(async () => {
    if (!onDelete || isBusy) {
      return;
    }
    try {
      setSaveError(null);
      await onDelete();
      onDismiss();
    } catch (error) {
      setSaveError(error instanceof Error && error.message ? error.message : `Failed to delete ${ruleNoun}.`);
    }
  }, [isBusy, onDelete, onDismiss, ruleNoun]);

  const modalButtons = [
    ...(mode === "edit" && onDelete
      ? [
          {
            text: "Delete",
            variant: variants.secondaryDanger,
            disabled: isBusy,
            loading: deleting,
            dismissModalOnClick: false,
            onClick: () => void handleDelete(),
          },
        ]
      : []),
    {
      text: "Save",
      variant: variants.primary,
      disabled: isSaveUnavailable,
      loading: saving,
      dismissModalOnClick: false,
      onClick: () => void handleSave(),
    },
  ];

  return { saveError, handleFormKeyDown, modalButtons };
}
