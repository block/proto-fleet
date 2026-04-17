import { BrowserRouter } from "react-router-dom";
import type { Meta, StoryObj } from "@storybook/react";
import FleetErrors from "./FleetErrors";

const meta: Meta<typeof FleetErrors> = {
  title: "Proto Fleet/Dashboard/FleetErrors",
  component: FleetErrors,
  parameters: {
    withRouter: false,
    layout: "padded",
    docs: {
      description: {
        component:
          "Displays error status for all hardware component types in the fleet (Control Boards, Fans, Hashboards, Power Supplies). Shows count of miners needing attention for each component type. Each box links to a filtered view of the miners page showing only miners with issues for that specific component. Responsive layout: 4 columns on desktop, 2 columns on tablet, 1 column on mobile.",
      },
    },
  },
  tags: ["autodocs"],
  argTypes: {
    controlBoardErrors: {
      control: "number",
      description: "Number of control board errors (0 displays 'No issues', undefined shows loading state)",
    },
    fanErrors: {
      control: "number",
      description: "Number of fan errors (0 displays 'No issues', undefined shows loading state)",
    },
    hashboardErrors: {
      control: "number",
      description: "Number of hashboard errors (0 displays 'No issues', undefined shows loading state)",
    },
    psuErrors: {
      control: "number",
      description: "Number of PSU errors (0 displays 'No issues', undefined shows loading state)",
    },
    className: {
      control: "text",
      description: "Optional CSS classes for styling",
    },
  },
  decorators: [
    (Story) => (
      <BrowserRouter>
        <div className="p-4">
          <Story />
        </div>
      </BrowserRouter>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof FleetErrors>;

export const Default: Story = {
  args: {
    controlBoardErrors: 0,
    fanErrors: 42,
    hashboardErrors: 58,
    psuErrors: 0,
  },
};

export const Loading: Story = {
  args: {
    controlBoardErrors: undefined,
    fanErrors: undefined,
    hashboardErrors: undefined,
    psuErrors: undefined,
  },
};

export const NoErrors: Story = {
  args: {
    controlBoardErrors: 0,
    fanErrors: 0,
    hashboardErrors: 0,
    psuErrors: 0,
  },
};

export const AllErrors: Story = {
  args: {
    controlBoardErrors: 12,
    fanErrors: 42,
    hashboardErrors: 58,
    psuErrors: 7,
  },
};
