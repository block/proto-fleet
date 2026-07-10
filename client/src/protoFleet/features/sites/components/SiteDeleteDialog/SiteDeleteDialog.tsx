import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";

interface SiteDeleteDialogProps {
  open: boolean;
  site: SiteWithCounts;
  onConfirm: () => void;
  onDismiss: () => void;
  deleting?: boolean;
}

// Pluralize helper kept local to the cascade copy; the same singular/plural
// rule applies to all the count rows so a shared helper is clearer than
// inlining ternaries each time.
const noun = (n: bigint, singular: string, plural: string) => (n === 1n ? singular : plural);

const buildCascadeSummary = (site: SiteWithCounts): string => {
  const { deviceCount, rackCount, buildingCount, infrastructureDeviceCount } = site;
  const base = `Deleting will unassign ${deviceCount} ${noun(deviceCount, "miner", "miners")}, ${rackCount} ${noun(rackCount, "rack", "racks")}, and ${buildingCount} ${noun(buildingCount, "building", "buildings")}. They will be removed from this site.`;
  if (infrastructureDeviceCount > 0n) {
    // Infrastructure devices (facility fans) are deleted with the site,
    // not unassigned, so they get their own sentence.
    return `${base} ${infrastructureDeviceCount} infrastructure ${noun(infrastructureDeviceCount, "device", "devices")} will also be deleted.`;
  }
  return base;
};

const SiteDeleteDialog = ({ open, site, onConfirm, onDismiss, deleting = false }: SiteDeleteDialogProps) => {
  const name = site.site?.name ?? "(unnamed)";
  const hasCascade =
    site.deviceCount > 0n || site.rackCount > 0n || site.buildingCount > 0n || site.infrastructureDeviceCount > 0n;
  const subtitle = hasCascade ? buildCascadeSummary(site) : "Are you sure you want to delete this site?";

  return (
    <Dialog
      open={open}
      title={`Delete site "${name}"?`}
      subtitle={subtitle}
      onDismiss={deleting ? undefined : onDismiss}
      testId="site-delete-dialog"
      buttons={[
        {
          text: "Cancel",
          variant: variants.secondary,
          onClick: onDismiss,
          disabled: deleting,
          testId: "site-delete-dialog-cancel",
        },
        {
          text: deleting ? "Deleting…" : "Delete site",
          variant: variants.danger,
          onClick: onConfirm,
          disabled: deleting,
          testId: "site-delete-dialog-confirm",
        },
      ]}
    />
  );
};

export default SiteDeleteDialog;
