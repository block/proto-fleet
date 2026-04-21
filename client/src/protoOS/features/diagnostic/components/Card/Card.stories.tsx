import type { Meta, StoryObj } from "@storybook/react";
import Card from "./Card";

const meta: Meta<typeof Card> = {
  title: "ProtoOS/Diagnostic/Card",
  component: Card,
  tags: ["autodocs"],
  args: {
    children: (
      <div>
        <div className="text-lg font-semibold">Card Title</div>
        <div className="text-base text-text-primary-70">
          This is a sample card content. You can put any React node here.
        </div>
      </div>
    ),
  },
};

export default meta;

type Story = StoryObj<typeof Card>;

export const Default: Story = {};
