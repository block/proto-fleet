import { useState } from "react";
import type { Meta, StoryObj } from "@storybook/react";
import { action } from "storybook/actions";

import ActionSheet, { type ActionSheetItem } from ".";
import Button, { sizes, variants } from "@/shared/components/Button";

const baseItems: ActionSheetItem[] = [
  { label: "Manage columns", onClick: action("Manage columns") },
  { label: "Export CSV", onClick: action("Export CSV") },
  { label: "Edit site", onClick: action("Edit site"), showGroupDivider: true },
  { label: "Unpair miners", danger: true, onClick: action("Unpair miners") },
];

const ActionSheetStory = ({ items = baseItems }: { items?: ActionSheetItem[] }) => {
  const [open, setOpen] = useState(true);

  return (
    <div className="flex min-h-screen items-center justify-center bg-surface-base p-6">
      <Button
        size={sizes.compact}
        variant={variants.secondary}
        text="Show action sheet"
        onClick={() => setOpen(true)}
      />
      {open ? <ActionSheet items={items} onClose={() => setOpen(false)} /> : null}
    </div>
  );
};

const meta = {
  title: "Shared/ActionSheet",
  component: ActionSheetStory,
  parameters: {
    layout: "fullscreen",
  },
  tags: ["autodocs"],
} satisfies Meta<typeof ActionSheetStory>;

export default meta;

type Story = StoryObj<typeof meta>;

export const Default: Story = {};

export const Scrollable: Story = {
  args: {
    items: [
      ...Array.from({ length: 16 }, (_, index) => ({
        label: `Action ${index + 1}`,
        onClick: action(`Action ${index + 1}`),
      })),
      { label: "Delete selection", danger: true, onClick: action("Delete selection") },
    ],
  },
};
