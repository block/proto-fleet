import { type ReactElement, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import { devChannelDraft, devChannelPreview, releaseChannels } from "./releaseChannel.fixtures";
import type { ReleaseChannel, ReleaseChannelDraft } from "./releaseChannelTypes";
import { FirmwareSettingsSurface } from "@/protoFleet/features/rollout/activeRolloutStoryHelpers";
import ReleaseChannelModal from "@/protoFleet/features/rollout/ReleaseChannelModal";
import ReleaseChannelsTable from "@/protoFleet/features/rollout/ReleaseChannelsTable";

/**
 * The firmware **release channels** surface: the "Files / Release channels" tab
 * added to the Firmware settings page, and the full-screen Manage release
 * channel modal a channel opens into. Both render **in situ** — the tab on the
 * real Firmware settings page (nav, subnav, header), the modal over it — the
 * same in-situ treatment the rest of the rollout stories use. Built on shipped
 * primitives (TabStrip, FullScreenTwoPaneModal, List, TargetSelectButton) plus
 * the framework's own RolloutControls.
 */
const meta = {
  title: "Proto Fleet/Rollout/In Situ/Release Channels",
  parameters: {
    layout: "fullscreen",
    // FirmwareSettingsSurface provides its own MemoryRouter.
    withRouter: false,
  },
} satisfies Meta;

export default meta;

type Story = StoryObj;

const noop = () => undefined;

/**
 * The Release channels tab live on the Firmware settings page, with the Manage
 * release channel modal wired to open from Create / a row's Manage button.
 */
function ReleaseChannelsTabInSitu(): ReactElement {
  const [modalOpen, setModalOpen] = useState(false);
  const [draft, setDraft] = useState<ReleaseChannelDraft>(devChannelDraft);

  const openForManage = (channel: ReleaseChannel) => {
    setDraft({ ...devChannelDraft, name: channel.name });
    setModalOpen(true);
  };

  return (
    <>
      <FirmwareSettingsSurface
        initialTab="releaseChannels"
        releaseChannelsTab={
          <ReleaseChannelsTable
            channels={releaseChannels}
            onCreate={() => {
              setDraft({ ...devChannelDraft, name: "", description: "" });
              setModalOpen(true);
            }}
            onManage={openForManage}
          />
        }
      />
      <ReleaseChannelModal
        open={modalOpen}
        mode="manage"
        draft={draft}
        onDraftChange={setDraft}
        preview={devChannelPreview}
        onAddFile={noop}
        onFileActions={noop}
        onSelectScope={noop}
        onDismiss={() => setModalOpen(false)}
        onSave={() => setModalOpen(false)}
      />
    </>
  );
}

/**
 * The Manage release channel modal open on its own (over the settings page),
 * pre-filled with the Dev channel from the mock.
 */
function ManageReleaseChannelInSitu(): ReactElement {
  const [draft, setDraft] = useState<ReleaseChannelDraft>(devChannelDraft);

  return (
    <>
      <FirmwareSettingsSurface
        initialTab="releaseChannels"
        releaseChannelsTab={<ReleaseChannelsTable channels={releaseChannels} onCreate={noop} onManage={noop} />}
      />
      <ReleaseChannelModal
        open
        mode="manage"
        draft={draft}
        onDraftChange={setDraft}
        preview={devChannelPreview}
        onAddFile={noop}
        onFileActions={noop}
        onSelectScope={noop}
        onDismiss={noop}
        onSave={noop}
      />
    </>
  );
}

/** The Release channels tab in situ on the Firmware settings page. */
export const ReleaseChannelsTabStory: Story = {
  name: "Release channels tab",
  render: () => <ReleaseChannelsTabInSitu />,
};

/** The Manage release channel modal, pre-filled from the Dev channel. */
export const ManageReleaseChannelStory: Story = {
  name: "Manage release channel",
  render: () => <ManageReleaseChannelInSitu />,
};
