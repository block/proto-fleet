import { Link, useNavigate } from "react-router-dom";

import type { DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import DeviceSetActionsMenu from "@/protoFleet/features/groupManagement/components/DeviceSetActionsMenu";
import { variants } from "@/shared/components/Button";

type GroupNameCellProps = {
  group: DeviceSet;
  onEdit: (group: DeviceSet) => void;
  onActionComplete?: () => void;
  href: string;
};

const GroupNameCell = ({ group, onEdit, onActionComplete, href }: GroupNameCellProps) => {
  const navigate = useNavigate();

  return (
    <div className="grid w-full grid-cols-[1fr_auto] items-center gap-3">
      <Link to={href} className="min-w-0 truncate text-left hover:underline" title={group.label}>
        {group.label}
      </Link>
      <DeviceSetActionsMenu
        deviceSetId={group.id}
        onEdit={() => onEdit(group)}
        onView={() => navigate(href)}
        onActionComplete={onActionComplete}
        buttonVariant={variants.textOnly}
      />
    </div>
  );
};

export default GroupNameCell;
