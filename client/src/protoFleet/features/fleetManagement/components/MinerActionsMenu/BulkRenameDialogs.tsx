import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";

interface BaseBulkRenameDialogsProps {
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

type BulkRenameDialogsProps =
  | (BaseBulkRenameDialogsProps & {
      showOverwriteWarning?: false;
    })
  | (BaseBulkRenameDialogsProps & {
      showOverwriteWarning: true;
      overwriteDialogTitle?: string;
      overwriteDialogBody: string;
      onDismissOverwriteWarning: () => void;
      onContinueOverwriteWarning: () => void;
    });

const BulkRenameDialogs = (props: BulkRenameDialogsProps) => {
  const {
    open,
    showDuplicateNamesWarning,
    showNoChangesWarning,
    duplicateNamesDialogBody,
    noChangesDialogBody,
    onDismissDuplicateNames,
    onContinueDuplicateNames,
    onDismissNoChanges,
    onContinueNoChanges,
  } = props;

  return (
    <>
      {showDuplicateNamesWarning ? (
        <Dialog
          open={open}
          title="Duplicate names"
          titleSize="text-heading-200"
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
          titleSize="text-heading-200"
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

      {props.showOverwriteWarning ? (
        <Dialog
          open={open}
          title={props.overwriteDialogTitle ?? "Overwrite existing names?"}
          titleSize="text-heading-200"
          subtitle={props.overwriteDialogBody}
          subtitleSize="text-300"
          subtitleClassName="text-text-primary-70"
          testId="bulk-rename-overwrite-dialog"
          buttons={[
            {
              text: "No, cancel",
              variant: variants.secondary,
              onClick: props.onDismissOverwriteWarning,
            },
            {
              text: "Yes, continue",
              variant: variants.primary,
              onClick: props.onContinueOverwriteWarning,
            },
          ]}
        />
      ) : null}
    </>
  );
};

export default BulkRenameDialogs;
