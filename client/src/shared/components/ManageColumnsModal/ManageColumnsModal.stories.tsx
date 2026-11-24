import { useState } from "react";
import type { Meta, StoryObj } from "@storybook/react";
import ManageColumnsModal, { type ColumnPreference } from "./ManageColumnsModal";

const defaults: ColumnPreference[] = [
  { id: "model", visible: true },
  { id: "status", visible: true },
  { id: "power", visible: true },
];
const Example = () => {
  const [columns, setColumns] = useState(defaults);
  return (
    <ManageColumnsModal
      columns={columns}
      defaultColumns={defaults}
      titles={{ model: "Model", status: "Status", power: "Power" }}
      onDismiss={() => {}}
      onSave={setColumns}
    />
  );
};
const meta = { title: "Shared/ManageColumnsModal", component: Example } satisfies Meta<typeof Example>;
export default meta;
export const Default: StoryObj<typeof meta> = {};
