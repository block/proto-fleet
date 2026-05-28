import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";

interface BuildingDeleteDialogProps {
  open: boolean;
  building: BuildingWithCounts;
  // Parent site name surfaces in the cascade copy so the operator
  // understands what the unassigned racks fall back to. Optional —
  // the dialog falls back to "this site" when unknown.
  parentSiteName?: string;
  onConfirm: () => void;
  onDismiss: () => void;
  deleting?: boolean;
}

const noun = (n: bigint, singular: string, plural: string) => (n === 1n ? singular : plural);

const buildCascadeSummary = (building: BuildingWithCounts, parentSiteName?: string): string => {
  const { rackCount } = building;
  const target = parentSiteName ? `"${parentSiteName}"` : "this site";
  // Rack-only language for PR 3 — `BuildingWithCounts` does not
  // expose device_count. The plan's "indirect device impact" line
  // depends on a follow-up that extends the response shape; see plan
  // §450 for the working answer.
  return `Deleting will unassign ${rackCount} ${noun(rackCount, "rack", "racks")} from this building and clear their zone labels. They will remain directly assigned to ${target}.`;
};

const BuildingDeleteDialog = ({
  open,
  building,
  parentSiteName,
  onConfirm,
  onDismiss,
  deleting = false,
}: BuildingDeleteDialogProps) => {
  const name = building.building?.name ?? "(unnamed)";
  const subtitle =
    building.rackCount > 0n
      ? buildCascadeSummary(building, parentSiteName)
      : "Are you sure you want to delete this building?";

  return (
    <Dialog
      open={open}
      title={`Delete building "${name}"?`}
      subtitle={subtitle}
      onDismiss={deleting ? undefined : onDismiss}
      testId="building-delete-dialog"
      buttons={[
        {
          text: "Cancel",
          variant: variants.secondary,
          onClick: onDismiss,
          disabled: deleting,
          testId: "building-delete-dialog-cancel",
        },
        {
          text: deleting ? "Deleting…" : "Delete building",
          variant: variants.danger,
          onClick: onConfirm,
          disabled: deleting,
          testId: "building-delete-dialog-confirm",
        },
      ]}
    />
  );
};

export default BuildingDeleteDialog;
