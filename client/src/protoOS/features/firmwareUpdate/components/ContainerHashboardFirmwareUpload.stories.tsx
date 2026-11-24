import type { Meta, StoryObj } from "@storybook/react-vite";
import ContainerHashboardFirmwareUpload from "./ContainerHashboardFirmwareUpload";

const meta: Meta<typeof ContainerHashboardFirmwareUpload> = {
  title: "Proto OS/Firmware Update/Container hashboard local upload",
  component: ContainerHashboardFirmwareUpload,
  parameters: { layout: "centered" },
  args: {
    deviceId: "storybook-container",
    firmware: {
      readImage: async (file) => ({ file, build: "prod", version: "9.0.0" }),
      prepare: async () => [{ mac: "02:00:00:00:00:01", owner: "controller", label: "Tank 1 · Module 1 · Board 1" }],
      install: async (_image, _attempt, progress, _params, starting) => {
        progress(100);
        starting();
      },
      progress: async (attempt) => ({
        outcome: "completed",
        errors: [],
        boards: attempt.boards.map((board) => ({ ...board, status: "succeeded", percent: 100 })),
      }),
      cancel: async () => ({ pending: false, finished: false, absent: [] }),
    },
  },
};
export default meta;
type Story = StoryObj<typeof meta>;
export const ChooseFile: Story = {};
