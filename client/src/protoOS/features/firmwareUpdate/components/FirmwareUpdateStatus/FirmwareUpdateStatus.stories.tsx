import type { Meta, StoryObj } from "@storybook/react-vite";
import FirmwareUpdateStatus from "./FirmwareUpdateStatus";
import { UpdateStatus } from "@/protoOS/api/generatedApi";

const meta: Meta<typeof FirmwareUpdateStatus> = {
  title: "ProtoOS/Firmware Update/FirmwareUpdateStatus",
  component: FirmwareUpdateStatus,
  parameters: {
    layout: "centered",
  },
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof meta>;

export const Available: Story = {
  args: {
    updateStatus: {
      status: "available",
      current_version: "1.0.0",
      new_version: "1.1.0",
      message: "Update available",
    } as UpdateStatus,
  },
};

export const Current: Story = {
  args: {
    updateStatus: {
      status: "current",
      current_version: "1.0.0",
      message: "Firmware is up to date",
    } as UpdateStatus,
  },
};

export const Downloading: Story = {
  args: {
    installing: true,
    updateStatus: {
      status: "downloading",
      current_version: "1.0.0",
      new_version: "1.1.0",
      progress: 45,
      message: "Downloading update",
    } as UpdateStatus,
  },
};

export const Downloaded: Story = {
  args: {
    installing: true,
    updateStatus: {
      status: "downloaded",
      current_version: "1.0.0",
      new_version: "1.1.0",
      message: "Ready to install",
    } as UpdateStatus,
  },
};

export const Installing: Story = {
  args: {
    installing: true,
    updateStatus: {
      status: "installing",
      current_version: "1.0.0",
      new_version: "1.1.0",
      progress: 75,
      message: "Installing update",
    } as UpdateStatus,
  },
};

export const Installed: Story = {
  args: {
    installing: false,
    updateStatus: {
      status: "installed",
      message: "Update installed",
    } as UpdateStatus,
  },
};

export const Error: Story = {
  args: {
    updateStatus: {
      status: "error",
      current_version: "1.0.0",
      error: "Download failed",
      message: "Update failed",
    } as UpdateStatus,
  },
};

export const Success: Story = {
  args: {
    updateStatus: {
      status: "success",
      current_version: "1.1.0",
      message: "Update completed",
    } as UpdateStatus,
  },
};

export const Loading: Story = {
  args: {
    loading: true,
  },
};
