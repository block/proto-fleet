import { useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { action } from "storybook/actions";
import InlineSelector from "./InlineSelector";
import Button from "@/shared/components/Button";
import { PopoverProvider } from "@/shared/components/Popover";

const meta = {
  title: "Shared/Inline Selector",
  component: InlineSelector,
  decorators: [
    (Story) => (
      <PopoverProvider>
        <div className="p-6">
          <Story />
        </div>
      </PopoverProvider>
    ),
  ],
  args: {
    items: [
      { id: "board-1", name: "Hashboard 1" },
      { id: "board-2", name: "Hashboard 2" },
      { id: "board-3", name: "Hashboard 3" },
    ],
    currentId: "board-1",
    onSelect: action("Select"),
  },
} satisfies Meta<typeof InlineSelector>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};
export const Empty: Story = { args: { items: [], currentId: undefined } };

export const OptionsChange: Story = {
  render: function OptionsChange(args) {
    const [hasItems, setHasItems] = useState(true);
    const [currentId, setCurrentId] = useState(args.currentId);
    return (
      <div className="flex flex-col items-start gap-6">
        <Button variant="secondary" onClick={() => setHasItems((current) => !current)}>
          {hasItems ? "Remove options" : "Restore options"}
        </Button>
        <InlineSelector {...args} items={hasItems ? args.items : []} currentId={currentId} onSelect={setCurrentId} />
      </div>
    );
  },
};
