import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";

interface RevokeNodeDialogProps {
  open?: boolean;
  nodeName: string;
  affectedDeviceTypes: string[];
  isLoadingImpact: boolean;
  impactError: string;
  onConfirm: () => void;
  onDismiss: () => void;
  isSubmitting: boolean;
}

const minerTypeLabels = new Map<string, [singular: string, plural: string]>([
  ["proto", ["Proto Rig", "Proto Rigs"]],
  ["antminer", ["Antminer", "Antminers"]],
  ["asicrs", ["ASIC-RS miner", "ASIC-RS miners"]],
  ["virtual", ["virtual miner", "virtual miners"]],
  ["whatsminer", ["WhatsMiner", "WhatsMiners"]],
  ["unknown", ["miner of unknown type", "miners of unknown type"]],
]);

const RevokeNodeDialog = ({
  open,
  nodeName,
  affectedDeviceTypes,
  isLoadingImpact,
  impactError,
  onConfirm,
  onDismiss,
  isSubmitting,
}: RevokeNodeDialogProps) => {
  const countsByType = new Map<string, number>();
  for (const deviceType of affectedDeviceTypes) {
    const type = deviceType.trim().toLowerCase() || "unknown";
    countsByType.set(type, (countsByType.get(type) ?? 0) + 1);
  }
  const groupedTypes = [...countsByType].sort(([first], [second]) => first.localeCompare(second));

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
        groupedTypes.length > 0 ? (
          <ul className="mt-3 max-h-32 list-disc overflow-y-auto pl-5 text-300">
            {groupedTypes.map(([type, count]) => {
              const [singular, plural] = minerTypeLabels.get(type) ?? [`${type} miner`, `${type} miners`];
              return (
                <li key={type}>
                  {count} paired {count === 1 ? singular : plural} will lose connection.
                </li>
              );
            })}
          </ul>
        ) : (
          <div className="mt-3 text-300">No paired miners will lose connection.</div>
        )
      ) : null}
    </Dialog>
  );
};

export default RevokeNodeDialog;
