import type { Meta, StoryObj } from "@storybook/react-vite";
import CT1FirmwareUpload from "./CT1FirmwareUpload";

const meta: Meta<typeof CT1FirmwareUpload> = {
  title: "Proto OS/Firmware Update/CT1 local upload",
  component: CT1FirmwareUpload,
  parameters: { layout: "centered" },
  args: {
    deviceId: "storybook-ct1",
    firmware: {
      prepare: async () => [{ mac: "02:00:00:00:00:01", owner: "controller", label: "Tank 1 · Module 1 · Board 1" }],
      install: async (_image, _attempt, progress, _params, starting) => {
        progress(100);
        starting();
      },
      progress: async (attempt) => ({
        complete: true,
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
