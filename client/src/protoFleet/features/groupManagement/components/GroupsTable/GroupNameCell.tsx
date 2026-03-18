import { Link } from "react-router-dom";

import type { DeviceCollection } from "@/protoFleet/api/generated/collection/v1/collection_pb";
import GroupActionsMenu from "@/protoFleet/features/groupManagement/components/GroupActionsMenu";
import { variants } from "@/shared/components/Button";

type GroupNameCellProps = {
  group: DeviceCollection;
  onEdit: (group: DeviceCollection) => void;
  onActionComplete?: () => void;
};

const GroupNameCell = ({ group, onEdit, onActionComplete }: GroupNameCellProps) => {
  return (
    <div className="grid w-full grid-cols-[1fr_auto] items-center gap-3">
      <Link
        to={`/groups/${encodeURIComponent(group.label)}`}
        className="min-w-0 truncate text-left hover:underline"
        title={group.label}
      >
        {group.label}
      </Link>
      <GroupActionsMenu
        collectionId={group.id}
        onEditGroup={() => onEdit(group)}
        onActionComplete={onActionComplete}
        buttonVariant={variants.textOnly}
      />
    </div>
  );
};

export default GroupNameCell;
