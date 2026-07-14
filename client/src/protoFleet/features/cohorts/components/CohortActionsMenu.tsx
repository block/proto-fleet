import RowActionsMenu, {
  type RowAction,
} from "@/protoFleet/features/fleetManagement/components/RowActionsMenu/RowActionsMenu";
import { Calendar, Lock, MiningPools, Plus, Repair, Trash } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";

interface CohortActionsMenuProps {
  disabled?: boolean;
  firmwareDisabled?: boolean;
  poolsDisabled?: boolean;
  mutationDisabled?: boolean;
  isSuperAdmin?: boolean;
  firmwareLabel?: string;
  lifecycleActionsHidden?: boolean;
  onFirmware: () => void;
  onPools: () => void;
  onExtend: () => void;
  onRelease: () => void;
  onAdminReassign: () => void;
}

const CohortActionsMenu = ({
  disabled,
  firmwareDisabled,
  poolsDisabled,
  mutationDisabled,
  isSuperAdmin = false,
  firmwareLabel = "Firmware",
  lifecycleActionsHidden = false,
  onFirmware,
  onPools,
  onExtend,
  onRelease,
  onAdminReassign,
}: CohortActionsMenuProps) => {
  const actions: RowAction[] = [
    {
      label: firmwareLabel,
      icon: <Repair />,
      onClick: onFirmware,
      disabled: firmwareDisabled,
      testId: "cohort-action-firmware",
    },
    {
      label: "Pools",
      icon: <MiningPools />,
      onClick: onPools,
      disabled: poolsDisabled,
      testId: "cohort-action-pools",
      showGroupDivider: !lifecycleActionsHidden,
    },
    {
      label: "Extend",
      icon: <Calendar />,
      onClick: onExtend,
      hidden: lifecycleActionsHidden,
      disabled: mutationDisabled,
      testId: "cohort-action-extend",
    },
    {
      label: "Release",
      icon: <Trash />,
      onClick: onRelease,
      hidden: lifecycleActionsHidden,
      disabled: mutationDisabled,
      testId: "cohort-action-release",
      showGroupDivider: isSuperAdmin,
    },
    {
      label: "Super admin reassign",
      icon: isSuperAdmin ? <Plus /> : <Lock />,
      onClick: onAdminReassign,
      hidden: lifecycleActionsHidden || !isSuperAdmin,
      disabled: mutationDisabled,
      testId: "cohort-action-reassign",
    },
  ];

  return (
    <RowActionsMenu
      actions={actions}
      ariaLabel="Cohort actions"
      testIdPrefix="cohort-actions"
      triggerVariant={variants.secondary}
      disabled={disabled}
    />
  );
};

export default CohortActionsMenu;
