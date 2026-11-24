import type { Meta, StoryObj } from "@storybook/react-vite";
import LocalFirmwareUpload from "./LocalFirmwareUpload";

const meta: Meta<typeof LocalFirmwareUpload> = {
  title: "Proto OS/Firmware Update/Local upload",
  component: LocalFirmwareUpload,
  parameters: { layout: "centered" },
  args: { pending: false, onUpload: async () => true },
};

export default meta;
type Story = StoryObj<typeof meta>;

export const ChooseFile: Story = {};
export const UpdateInProgress: Story = { args: { disabled: true } };
export const RejectedUpload: Story = {
  args: {
    error: "The rig rejected this firmware file. Choose a valid R2 .swu file.",
    onUpload: async () => false,
  },
};
