import RowActionsMenu, {
  type RowAction,
} from "@/protoFleet/features/fleetManagement/components/RowActionsMenu/RowActionsMenu";
import { Calendar, Lock, Plus, Settings, Trash } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";

interface CohortActionsMenuProps {
  disabled?: boolean;
  firmwareDisabled?: boolean;
  mutationDisabled?: boolean;
  isSuperAdmin?: boolean;
  onFirmware: () => void;
  onExtend: () => void;
  onRelease: () => void;
  onAdminReassign: () => void;
}

const CohortActionsMenu = ({
  disabled,
  firmwareDisabled,
  mutationDisabled,
  isSuperAdmin = false,
  onFirmware,
  onExtend,
  onRelease,
  onAdminReassign,
}: CohortActionsMenuProps) => {
  const actions: RowAction[] = [
    {
      label: "Firmware",
      icon: <Settings />,
      onClick: onFirmware,
      disabled: firmwareDisabled,
      testId: "cohort-action-firmware",
    },
    {
      label: "Extend",
      icon: <Calendar />,
      onClick: onExtend,
      disabled: mutationDisabled,
      testId: "cohort-action-extend",
    },
    {
      label: "Release",
      icon: <Trash />,
      onClick: onRelease,
      disabled: mutationDisabled,
      testId: "cohort-action-release",
      showGroupDivider: isSuperAdmin,
    },
    {
      label: "Super admin reassign",
      icon: isSuperAdmin ? <Plus /> : <Lock />,
      onClick: onAdminReassign,
      hidden: !isSuperAdmin,
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
