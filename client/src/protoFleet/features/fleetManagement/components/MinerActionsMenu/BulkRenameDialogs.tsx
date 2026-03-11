import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";

interface BulkRenameDialogsProps {
  open: boolean;
  showDuplicateNamesWarning: boolean;
  showNoChangesWarning: boolean;
  duplicateNamesDialogBody: string;
  noChangesDialogBody: string;
  onDismissDuplicateNames: () => void;
  onContinueDuplicateNames: () => void;
  onDismissNoChanges: () => void;
  onContinueNoChanges: () => void;
}

const BulkRenameDialogs = ({
  open,
  showDuplicateNamesWarning,
  showNoChangesWarning,
  duplicateNamesDialogBody,
  noChangesDialogBody,
  onDismissDuplicateNames,
  onContinueDuplicateNames,
  onDismissNoChanges,
  onContinueNoChanges,
}: BulkRenameDialogsProps) => (
  <>
    {showDuplicateNamesWarning ? (
      <Dialog
        open={open}
        title="Duplicate names"
        subtitle={duplicateNamesDialogBody}
        subtitleSize="text-300"
        subtitleClassName="text-text-primary-70"
        testId="bulk-rename-duplicate-names-dialog"
        buttons={[
          {
            text: "No, keep editing",
            variant: variants.secondary,
            onClick: onDismissDuplicateNames,
          },
          {
            text: "Yes, continue",
            variant: variants.primary,
            onClick: onContinueDuplicateNames,
          },
        ]}
      />
    ) : null}

    {showNoChangesWarning ? (
      <Dialog
        open={open}
        title="You haven't made any changes"
        subtitle={noChangesDialogBody}
        subtitleSize="text-300"
        subtitleClassName="text-text-primary-70"
        testId="bulk-rename-no-changes-dialog"
        buttons={[
          {
            text: "No, keep editing",
            variant: variants.secondary,
            onClick: onDismissNoChanges,
          },
          {
            text: "Yes, continue",
            variant: variants.primary,
            onClick: onContinueNoChanges,
          },
        ]}
      />
    ) : null}
  </>
);

export default BulkRenameDialogs;
