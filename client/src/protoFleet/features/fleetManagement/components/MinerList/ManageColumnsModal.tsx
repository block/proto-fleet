import { minerColTitles } from "./constants";
import {
  createDefaultMinerTableColumnPreferences,
  type MinerTableColumnPreference,
  type MinerTableColumnPreferences,
} from "./minerTableColumnPreferences";
import TableColumnsModal from "@/shared/components/ManageColumnsModal";

type Props = {
  preferences: MinerTableColumnPreferences;
  /** "Allocated to" only renders for hosted roots, so other fleets don't offer a toggle that does nothing. */
  showAssignedAccount: boolean;
  onDismiss: () => void;
  onSave: (preferences: MinerTableColumnPreferences) => void;
};

const offered = (columns: MinerTableColumnPreference[], showAssignedAccount: boolean) =>
  columns.filter((column) => showAssignedAccount || column.id !== "assignedAccount");

const ManageColumnsModal = ({ preferences, showAssignedAccount, onDismiss, onSave }: Props) => (
  <TableColumnsModal
    columns={offered(preferences.columns, showAssignedAccount)}
    defaultColumns={offered(createDefaultMinerTableColumnPreferences().columns, showAssignedAccount)}
    titles={minerColTitles}
    onDismiss={onDismiss}
    onSave={(columns) => {
      // Keep the hidden column's stored visibility and position.
      const index = preferences.columns.findIndex((column) => column.id === "assignedAccount");
      const saved = [...columns];
      if (!showAssignedAccount && index >= 0) saved.splice(index, 0, preferences.columns[index]);
      onSave({ columns: saved });
    }}
  />
);
export default ManageColumnsModal;
