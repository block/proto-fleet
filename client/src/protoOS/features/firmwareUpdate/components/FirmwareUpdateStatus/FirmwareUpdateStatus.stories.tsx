import type { Meta, StoryObj } from "@storybook/react-vite";
import FirmwareUpdateStatus from "./FirmwareUpdateStatus";
import type { FirmwareUpdateInfo } from "@/protoOS/store/contract";

const meta: Meta<typeof FirmwareUpdateStatus> = {
  title: "Proto OS/Firmware Update/FirmwareUpdateStatus",
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
      currentVersion: "1.0.0",
      newVersion: "1.1.0",
      message: "Update available",
    } as FirmwareUpdateInfo,
  },
};

export const Current: Story = {
  args: {
    updateStatus: {
      status: "current",
      currentVersion: "1.0.0",
      message: "Firmware is up to date",
    } as FirmwareUpdateInfo,
  },
};

export const Downloading: Story = {
  args: {
    installing: true,
    updateStatus: {
      status: "downloading",
      currentVersion: "1.0.0",
      newVersion: "1.1.0",
      progressPercent: 45,
      message: "Downloading update",
    } as FirmwareUpdateInfo,
  },
};

export const Downloaded: Story = {
  args: {
    installing: true,
    updateStatus: {
      status: "downloaded",
      currentVersion: "1.0.0",
      newVersion: "1.1.0",
      message: "Ready to install",
    } as FirmwareUpdateInfo,
  },
};

export const Installing: Story = {
  args: {
    installing: true,
    updateStatus: {
      status: "installing",
      currentVersion: "1.0.0",
      newVersion: "1.1.0",
      progressPercent: 75,
      message: "Installing update",
    } as FirmwareUpdateInfo,
  },
};

export const Installed: Story = {
  args: {
    installing: false,
    updateStatus: {
      status: "rebootRequired",
      message: "Update installed",
    } as FirmwareUpdateInfo,
  },
};

export const Error: Story = {
  args: {
    updateStatus: {
      status: "failed",
      currentVersion: "1.0.0",
      message: "Update failed",
    } as FirmwareUpdateInfo,
  },
};

export const Success: Story = {
  args: {
    updateStatus: {
      status: "complete",
      currentVersion: "1.1.0",
      message: "Update completed",
    } as FirmwareUpdateInfo,
  },
};

export const Loading: Story = {
  args: {
    loading: true,
  },
};
