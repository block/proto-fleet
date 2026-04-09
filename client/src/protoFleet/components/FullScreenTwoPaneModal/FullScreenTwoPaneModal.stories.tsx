import { useState } from "react";
import type { Meta, StoryObj } from "@storybook/react";

import FullScreenTwoPaneModal from "./FullScreenTwoPaneModal";
import { DismissCircle } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import ProgressCircular from "@/shared/components/ProgressCircular";

const SamplePane = ({ label, className }: { label: string; className?: string }) => (
  <section className={`flex flex-col gap-4 pt-6 pr-6 pb-6 ${className ?? ""}`}>
    <div className="text-emphasis-300 text-text-primary">{label}</div>
    <div className="rounded-lg bg-surface-5 p-6">
      <div className="text-300 text-text-primary-70">Sample content for {label.toLowerCase()}</div>
    </div>
    <div className="rounded-lg bg-surface-5 p-6">
      <div className="text-300 text-text-primary-70">Additional content block</div>
    </div>
  </section>
);

const PreviewPane = () => (
  <section className="flex flex-col gap-4 p-6">
    <div className="text-emphasis-300 text-text-primary">Preview</div>
    <div className="rounded-lg bg-surface-base p-6">
      <div className="text-300 text-text-primary-70">Preview content appears here</div>
    </div>
  </section>
);

type StoryArgs = {
  title: string;
  isBusy?: boolean;
  hasButtons?: boolean;
  maxWidth?: string;
  showAbovePanes?: boolean;
  showLoadingState?: boolean;
};

const FullScreenTwoPaneModalStory = ({
  title,
  isBusy,
  hasButtons = true,
  maxWidth,
  showAbovePanes,
  showLoadingState,
}: StoryArgs) => {
  const [open, setOpen] = useState(true);

  if (!open) {
    return (
      <div className="flex h-screen items-center justify-center bg-surface-base">
        <button onClick={() => setOpen(true)} className="bg-emphasis-300 rounded-lg px-4 py-2 text-surface-base">
          Show Modal
        </button>
      </div>
    );
  }

  return (
    <FullScreenTwoPaneModal
      open
      title={title}
      onDismiss={() => setOpen(false)}
      isBusy={isBusy}
      buttons={
        hasButtons
          ? [
              { text: "Secondary", variant: variants.secondary, onClick: () => {} },
              { text: "Save", variant: variants.primary, onClick: () => {}, disabled: isBusy },
            ]
          : undefined
      }
      maxWidth={maxWidth}
      abovePanes={
        showAbovePanes ? (
          <div className="mx-auto w-full max-w-[1280px] shrink-0 px-6 pb-4 laptop:px-0 desktop:px-0">
            <Callout
              intent="danger"
              prefixIcon={<DismissCircle />}
              title="Something went wrong. Please try again."
              dismissible
            />
          </div>
        ) : undefined
      }
      loadingState={
        showLoadingState ? (
          <div className="flex flex-1 items-center justify-center">
            <ProgressCircular indeterminate />
          </div>
        ) : undefined
      }
      primaryPane={<SamplePane label="Configuration" />}
      secondaryPane={<PreviewPane />}
    />
  );
};

const meta = {
  title: "Shared/FullScreenTwoPaneModal",
  component: FullScreenTwoPaneModalStory,
  parameters: {
    layout: "fullscreen",
  },
  tags: ["autodocs"],
} satisfies Meta<typeof FullScreenTwoPaneModalStory>;

export default meta;

type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: {
    title: "Full Screen Two Pane Modal",
  },
};

const WithOverflowButtonsRender = (args: StoryArgs) => {
  const [open, setOpen] = useState(true);

  if (!open) {
    return (
      <div className="flex h-screen items-center justify-center bg-surface-base">
        <button onClick={() => setOpen(true)} className="bg-emphasis-300 rounded-lg px-4 py-2 text-surface-base">
          Show Modal
        </button>
      </div>
    );
  }

  return (
    <FullScreenTwoPaneModal
      open
      title={args.title}
      onDismiss={() => setOpen(false)}
      buttons={[
        { text: "Delete", variant: variants.secondaryDanger, onClick: () => {} },
        { text: "Edit Settings", variant: variants.secondary, onClick: () => {} },
        { text: "Manage", variant: variants.secondary, onClick: () => {} },
        { text: "Save", variant: variants.primary, onClick: () => {} },
      ]}
      primaryPane={<SamplePane label="Configuration" />}
      secondaryPane={<PreviewPane />}
    />
  );
};

export const WithOverflowButtons: Story = {
  args: {
    title: "Modal with Overflow Menu",
    hasButtons: false,
  },
  render: WithOverflowButtonsRender,
};

export const BusyState: Story = {
  args: {
    title: "Saving Changes",
    isBusy: true,
    hasButtons: true,
  },
};

export const WithAbovePanesContent: Story = {
  args: {
    title: "Modal with Error",
    showAbovePanes: true,
  },
};

export const WithLoadingState: Story = {
  args: {
    title: "Loading Data",
    showLoadingState: true,
  },
};

export const WithMaxWidth: Story = {
  args: {
    title: "Constrained Width Modal",
    maxWidth: "1280px",
  },
};
