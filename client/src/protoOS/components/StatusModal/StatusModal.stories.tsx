import { useState } from "react";
import type { Meta, StoryObj } from "@storybook/react";
import { ProtoOSStatusModal } from "./index";

/**
 * ProtoOS StatusModal component stories
 *
 * Note: This component integrates with the ProtoOS store which is mocked in stories.
 * In production, it automatically fetches data from the store and handles all state management.
 */
const meta = {
  title: "Proto OS/StatusModal",
  component: ProtoOSStatusModal,
  parameters: {
    layout: "centered",
    docs: {
      description: {
        component:
          "ProtoOS-specific StatusModal wrapper that integrates with the store. " +
          "This component encapsulates all integration logic and provides a simple API for consumers.",
      },
    },
  },
  decorators: [
    (Story) => (
      <div style={{ minWidth: "500px", minHeight: "400px" }}>
        <Story />
      </div>
    ),
  ],
  argTypes: {
    open: {
      description: "Controls modal visibility",
      control: { type: "boolean" },
    },
    onClose: {
      description: "Callback when modal should be closed",
      action: "closed",
    },
    showBackButton: {
      description: "Whether to show back button in component views",
      control: { type: "boolean" },
    },
    componentAddress: {
      description: "Optional initial component to display",
      control: { type: "object" },
    },
  },
} satisfies Meta<typeof ProtoOSStatusModal>;

export default meta;
type Story = StoryObj<typeof meta>;

/**
 * Default story showing the modal in closed state
 */
export const Default: Story = {
  args: {
    open: false,
    onClose: () => {},
  },
};

/**
 * Modal shown with normal miner status
 * In production, this would show actual store data
 */
export const OpenNormal: Story = {
  args: {
    open: true,
    onClose: () => {},
  },
  parameters: {
    docs: {
      description: {
        story:
          "Modal open showing normal miner status. In production, this displays real-time data from the ProtoOS store.",
      },
    },
  },
};

/**
 * Modal with custom showBackButton setting
 */
export const WithoutBackButton: Story = {
  args: {
    open: true,
    onClose: () => {},
    showBackButton: false,
  },
  parameters: {
    docs: {
      description: {
        story: "Modal configured to hide the back button in component views.",
      },
    },
  },
};

/**
 * Interactive example showing how the modal is typically used
 */
export const Interactive: Story = {
  render: function Render(args) {
    const [isOpen, setIsOpen] = useState(false);

    return (
      <>
        <button onClick={() => setIsOpen(true)} className="bg-core-primary-30 rounded px-4 py-2 text-surface-base">
          Open Status Modal
        </button>

        <ProtoOSStatusModal
          {...args}
          open={isOpen}
          onClose={() => {
            setIsOpen(false);
            args.onClose();
          }}
        />
      </>
    );
  },
  args: {
    open: false,
    onClose: () => {},
  },
  parameters: {
    docs: {
      description: {
        story:
          "Interactive example demonstrating typical usage with a button trigger. " +
          "This is how the modal is used in MinerStatus and ErrorCallout components.",
      },
    },
  },
};

/**
 * Example with mocked store data showing errors
 * Note: In production, the component automatically fetches this from the store
 */
export const WithMockedErrors: Story = {
  args: {
    open: true,
    onClose: () => {},
  },
  parameters: {
    docs: {
      description: {
        story:
          "Example with mocked store data showing component errors. " +
          "In production, error data is automatically fetched from the ProtoOS store.",
      },
    },
  },
  // Note: To properly mock store data for stories, you would need to:
  // 1. Use a store provider wrapper
  // 2. Mock the store hooks (useErrors, useMinerStatusTitle, etc.)
  // 3. Provide test data through the mocked store
  // For now, this story will show the default store state
};

/**
 * Documentation example showing typical integration
 */
export const UsageExample: Story = {
  args: {
    open: false,
    onClose: () => {},
  },
  render: () => (
    <div className="bg-surface-raised rounded p-6">
      <h3 className="text-emphasis-500 mb-4 font-semibold">Usage Example:</h3>
      <pre className="rounded bg-surface-base p-4">
        <code>{`import { useState } from "react";
import { ProtoOSStatusModal } from "@/protoOS/components/StatusModal";

function MyComponent() {
  const [isModalOpen, setModalOpen] = useState(false);

  return (
    <>
      <button onClick={() => setModalOpen(true)}>
        View Status
      </button>

      <ProtoOSStatusModal
        open={isModalOpen}
        onClose={() => setModalOpen(false)}
      />
    </>
  );
}`}</code>
      </pre>
      <p className="mt-4 text-200 text-text-primary-70">
        The ProtoOSStatusModal automatically handles all store integration, wake miner functionality, and component
        navigation internally.
      </p>
    </div>
  ),
  parameters: {
    docs: {
      description: {
        story: "Code example showing how to integrate the ProtoOSStatusModal in your component.",
      },
    },
  },
};
