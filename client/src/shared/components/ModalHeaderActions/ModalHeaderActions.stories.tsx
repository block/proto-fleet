import type { Meta, StoryObj } from "@storybook/react";
import { action } from "storybook/actions";

import ModalHeaderActions from ".";
import { sizes, variants } from "@/shared/components/Button";

const ModalHeaderActionsStory = () => (
  <div className="flex min-h-screen items-start justify-center bg-surface-base p-6">
    <div className="w-full max-w-[520px] rounded-2xl bg-surface-elevated-base p-6 shadow-100">
      <div className="flex items-center justify-between gap-3">
        <div className="truncate text-heading-200 text-text-primary">Manage columns</div>
        <ModalHeaderActions
          buttonSize={sizes.compact}
          className="!block tablet:!block"
          renderWhen="always"
          buttons={[
            { text: "Reset to defaults", variant: variants.secondary, onClick: action("Reset to defaults") },
            { text: "Save", variant: variants.primary, onClick: action("Save") },
          ]}
        />
      </div>
    </div>
  </div>
);

const meta = {
  title: "Shared/ModalHeaderActions",
  component: ModalHeaderActionsStory,
  parameters: {
    layout: "fullscreen",
  },
  tags: ["autodocs"],
} satisfies Meta<typeof ModalHeaderActionsStory>;

export default meta;

type Story = StoryObj<typeof meta>;

export const Default: Story = {};
