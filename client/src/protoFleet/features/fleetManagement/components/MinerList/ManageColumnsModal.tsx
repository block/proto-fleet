import { minerColTitles } from "./constants";
import {
  createDefaultMinerTableColumnPreferences,
  type MinerTableColumnPreferences,
} from "./minerTableColumnPreferences";
import TableColumnsModal from "@/shared/components/ManageColumnsModal";

type Props = {
  preferences: MinerTableColumnPreferences;
  onDismiss: () => void;
  onSave: (preferences: MinerTableColumnPreferences) => void;
};

const ManageColumnsModal = ({ preferences, onDismiss, onSave }: Props) => (
  <TableColumnsModal
    columns={preferences.columns}
    defaultColumns={createDefaultMinerTableColumnPreferences().columns}
    titles={minerColTitles}
    onDismiss={onDismiss}
    onSave={(columns) => onSave({ columns })}
  />
);
export default ManageColumnsModal;
