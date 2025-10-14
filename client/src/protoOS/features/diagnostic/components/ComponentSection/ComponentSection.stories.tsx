import type { Meta, StoryObj } from "@storybook/react";
import ComponentSection from "./ComponentSection";

const meta: Meta<typeof ComponentSection> = {
  title: "ProtoOS/Diagnostic/ComponentSection",
  component: ComponentSection,
  parameters: {
    layout: "padded",
  },
  tags: ["autodocs"],
  argTypes: {
    title: {
      control: "text",
      description: "Section title",
    },
    children: {
      control: "text",
      description: "Section content",
    },
  },
};

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: {
    title: "Section Title",
    children: <div>This is some content.</div>,
  },
};
