import { BrowserRouter } from "react-router-dom";
import type { Meta, StoryObj } from "@storybook/react";
import ComponentErrors from "./ComponentErrors";
import ControlBoard from "@/shared/assets/icons/ControlBoard";
import Fan from "@/shared/assets/icons/Fan";
import Hashboard from "@/shared/assets/icons/Hashboard";
import LightningAlt from "@/shared/assets/icons/LightningAlt";

const meta: Meta<typeof ComponentErrors> = {
  title: "Proto Fleet/Dashboard/ComponentErrors",
  component: ComponentErrors,
  parameters: {
    withRouter: false,
    layout: "centered",
    docs: {
      description: {
        component: "Displays component-level error status for fleet hardware with icon and status message",
      },
    },
  },
  tags: ["autodocs"],
  argTypes: {
    icon: {
      control: false,
      description: "Icon component representing the hardware type",
    },
    heading: {
      control: "text",
      description: "The hardware component name",
    },
    errorCount: {
      control: "number",
      description: "Number of miners with errors (0 displays 'No issues', undefined shows loading state)",
    },
    href: {
      control: "text",
      description: "Optional link destination (renders as Link when provided)",
    },
    className: {
      control: "text",
      description: "Optional CSS classes for styling",
    },
  },
  decorators: [
    (Story) => (
      <BrowserRouter>
        <div className="w-[313px] p-4">
          <Story />
        </div>
      </BrowserRouter>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof ComponentErrors>;

export const Default: Story = {
  args: {
    icon: <ControlBoard />,
    heading: "Control Boards",
    errorCount: 0,
  },
};

export const Loading: Story = {
  args: {
    icon: <ControlBoard />,
    heading: "Control Boards",
    errorCount: undefined,
  },
};

export const WithErrors: Story = {
  args: {
    icon: <ControlBoard />,
    heading: "Control Boards",
    errorCount: 2,
  },
};

export const HashboardNoIssues: Story = {
  args: {
    icon: <Hashboard />,
    heading: "Hashboards",
    errorCount: 0,
  },
};

export const HashboardErrors: Story = {
  args: {
    icon: <Hashboard />,
    heading: "Hashboards",
    errorCount: 5,
  },
};

export const PSUNoIssues: Story = {
  args: {
    icon: <LightningAlt />,
    heading: "Power Supplies",
    errorCount: 0,
  },
};

export const PSUErrors: Story = {
  args: {
    icon: <LightningAlt />,
    heading: "Power Supplies",
    errorCount: 1,
  },
};

export const FanNoIssues: Story = {
  args: {
    icon: <Fan />,
    heading: "Fans",
    errorCount: 0,
  },
};

export const FanErrors: Story = {
  args: {
    icon: <Fan />,
    heading: "Fans",
    errorCount: 42,
  },
};

export const WithLink: Story = {
  args: {
    icon: <ControlBoard />,
    heading: "Control Boards",
    errorCount: 3,
    href: "/errors/control-boards",
  },
};

export const NoErrorsWithLink: Story = {
  args: {
    icon: <Hashboard />,
    heading: "Hashboards",
    errorCount: 0,
    href: "/errors/hashboards",
  },
};
