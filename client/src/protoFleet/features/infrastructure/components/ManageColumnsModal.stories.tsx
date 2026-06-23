import { useState } from "react";
import ManageColumnsModal, { type InfraColumnPreference } from "./ManageColumnsModal";
import Button, { variants } from "@/shared/components/Button";

export default {
  title: "Proto Fleet/Infrastructure/ManageColumnsModal",
  component: ManageColumnsModal,
};

const defaultColumns: InfraColumnPreference[] = [
  { id: "status", label: "Status", visible: true },
  { id: "lastSeen", label: "Last seen", visible: true },
  { id: "site", label: "Site", visible: true },
  { id: "building", label: "Building", visible: true },
  { id: "type", label: "Type", visible: true },
  { id: "enabled", label: "Enabled", visible: true },
  { id: "endpoint", label: "Endpoint", visible: true },
  { id: "port", label: "Port", visible: true },
  { id: "id", label: "ID", visible: true },
];

export const Default = () => {
  const [open, setOpen] = useState(true);
  const [columns, setColumns] = useState(defaultColumns);

  return (
    <>
      <Button variant={variants.primary} text="Open Modal" onClick={() => setOpen(true)} />
      {open ? (
        <ManageColumnsModal
          columns={columns}
          onDismiss={() => setOpen(false)}
          onSave={(updated) => {
            setColumns(updated);
            setOpen(false);
          }}
        />
      ) : null}
    </>
  );
};
