import type { ModuleListItem } from "./moduleListModel";
import { Alert } from "@/shared/assets/icons";
import RowActionsMenu, { type RowAction } from "@/shared/components/RowActionsMenu";

const ModuleName = ({
  row,
  actions,
  onViewErrors,
}: {
  row: ModuleListItem;
  actions: RowAction[];
  onViewErrors: () => void;
}) => (
  <div className="grid w-full grid-cols-[1fr_auto] items-center gap-3">
    <span className="truncate text-emphasis-300" title={row.name}>
      {row.name}
    </span>
    <div className="flex items-center gap-2">
      {row.hasErrors ? (
        <button
          type="button"
          aria-label={`View errors for ${row.name}`}
          onClick={onViewErrors}
          className="cursor-pointer hover:opacity-80"
        >
          <Alert width="w-4" className="text-intent-critical-fill" />
        </button>
      ) : null}
      <RowActionsMenu actions={actions} ariaLabel={`Actions for ${row.name}`} />
    </div>
  </div>
);

export default ModuleName;
