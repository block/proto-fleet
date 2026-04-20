import { Link, useNavigate } from "react-router-dom";

import type { DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import DeviceSetActionsMenu from "@/protoFleet/features/groupManagement/components/DeviceSetActionsMenu";
import { variants } from "@/shared/components/Button";

type GroupNameCellProps = {
  group: DeviceSet;
  onEdit: (group: DeviceSet) => void;
  onActionComplete?: () => void;
};

const GroupNameCell = ({ group, onEdit, onActionComplete }: GroupNameCellProps) => {
  const navigate = useNavigate();

  return (
    <div className="grid w-full grid-cols-[1fr_auto] items-center gap-3">
      <Link
        to={`/groups/${encodeURIComponent(group.label)}`}
        className="min-w-0 truncate text-left hover:underline"
        title={group.label}
      >
        {group.label}
      </Link>
      <DeviceSetActionsMenu
        deviceSetId={group.id}
        onEdit={() => onEdit(group)}
        onView={() => navigate(`/groups/${encodeURIComponent(group.label)}`)}
        onActionComplete={onActionComplete}
        buttonVariant={variants.textOnly}
      />
    </div>
  );
};

export default GroupNameCell;
