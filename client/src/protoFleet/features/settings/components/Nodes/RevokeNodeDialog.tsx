import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";

interface RevokeNodeDialogProps {
  open?: boolean;
  nodeName: string;
  affectedMiners: string[];
  isLoadingImpact: boolean;
  impactError: string;
  onConfirm: () => void;
  onDismiss: () => void;
  isSubmitting: boolean;
}

const RevokeNodeDialog = ({
  open,
  nodeName,
  affectedMiners,
  isLoadingImpact,
  impactError,
  onConfirm,
  onDismiss,
  isSubmitting,
}: RevokeNodeDialogProps) => {
  return (
    <Dialog
      open={open}
      title="Revoke node?"
      onDismiss={onDismiss}
      icon={
        <DialogIcon intent="critical">
          <Alert />
        </DialogIcon>
      }
      buttons={[
        {
          text: "Cancel",
          onClick: onDismiss,
          variant: variants.secondary,
          disabled: isSubmitting,
        },
        {
          text: "Revoke node",
          onClick: onConfirm,
          variant: variants.danger,
          loading: isSubmitting,
          disabled: isLoadingImpact || !!impactError || isSubmitting,
        },
      ]}
    >
      <div className="text-300 text-text-primary-70">
        Are you sure you want to revoke "{nodeName}"? The node immediately loses access to Fleet, and its miner pairings
        and stored miner credentials are removed. Its miners keep running, but another node will need to discover and
        pair them again. This action cannot be undone.
      </div>
      {isLoadingImpact ? <div className="mt-3 text-300">Loading affected miners…</div> : null}
      {impactError ? (
        <div role="alert" className="mt-3 text-300 text-intent-critical-fill">
          {impactError}
        </div>
      ) : null}
      {!isLoadingImpact && !impactError ? (
        <div className="mt-3 text-300">
          <div>
            {affectedMiners.length} paired {affectedMiners.length === 1 ? "miner" : "miners"} will lose this Node.
          </div>
          {affectedMiners.length > 0 ? (
            <ul className="mt-2 max-h-32 list-disc overflow-y-auto pl-5">
              {affectedMiners.map((identifier) => (
                <li key={identifier}>{identifier}</li>
              ))}
            </ul>
          ) : null}
        </div>
      ) : null}
    </Dialog>
  );
};

export default RevokeNodeDialog;
