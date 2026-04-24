import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import { CategorizedInvalidEntries } from "@/shared/utils/networkDiscovery";

interface ValidationErrorDialogProps {
  open?: boolean;
  invalidEntries: CategorizedInvalidEntries;
  hasValidEntries: boolean;
  onBackToEditing: () => void;
  onContinueAnyway: () => void;
}

const ValidationErrorDialog = ({
  open,
  invalidEntries,
  hasValidEntries,
  onBackToEditing,
  onContinueAnyway,
}: ValidationErrorDialogProps) => {
  const hasIpAddresses = invalidEntries.ipAddresses.length > 0;
  const hasIpRanges = invalidEntries.ipRanges.length > 0;
  const hasSubnets = invalidEntries.subnets.length > 0;

  return (
    <Dialog
      open={open}
      testId="validation-error-dialog"
      title="Some entries not recognized"
      onDismiss={onBackToEditing}
      subtitle={
        hasValidEntries
          ? "Review and fix these entries or continue without them."
          : "Review and fix these entries to continue."
      }
      subtitleSize="text-300"
      buttons={[
        {
          text: "Back to editing",
          onClick: onBackToEditing,
          variant: hasValidEntries ? variants.secondary : variants.primary,
        },
        ...(hasValidEntries
          ? [
              {
                text: "Continue anyway",
                onClick: onContinueAnyway,
                variant: variants.primary,
              },
            ]
          : []),
      ]}
    >
      <div className="flex flex-col gap-2 text-300 text-text-primary-70">
        {hasIpAddresses ? (
          <div>
            <p className="font-medium text-text-primary">Invalid IP addresses</p>
            <p>{invalidEntries.ipAddresses.join(", ")}</p>
          </div>
        ) : null}
        {hasIpRanges ? (
          <div>
            <p className="font-medium text-text-primary">Invalid IP ranges</p>
            <p>{invalidEntries.ipRanges.join(", ")}</p>
          </div>
        ) : null}
        {hasSubnets ? (
          <div>
            <p className="font-medium text-text-primary">Invalid subnet blocks</p>
            <p>{invalidEntries.subnets.join(", ")}</p>
          </div>
        ) : null}
      </div>
    </Dialog>
  );
};

export default ValidationErrorDialog;
