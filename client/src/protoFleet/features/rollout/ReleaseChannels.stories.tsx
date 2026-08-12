import { type ReactElement, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import { devChannelDraft, devChannelPreview, releaseChannels } from "./releaseChannel.fixtures";
import type { ReleaseChannel, ReleaseChannelDraft } from "./releaseChannelTypes";
import { FirmwareSettingsSurface } from "@/protoFleet/features/rollout/activeRolloutStoryHelpers";
import ReleaseChannelModal from "@/protoFleet/features/rollout/ReleaseChannelModal";
import ReleaseChannelsTable from "@/protoFleet/features/rollout/ReleaseChannelsTable";

/** Release channels rendered on the Firmware settings page. */
const meta = {
  title: "Proto Fleet/Firmware/Release Channels",
  parameters: {
    layout: "fullscreen",
    // FirmwareSettingsSurface provides its own MemoryRouter.
    withRouter: false,
  },
} satisfies Meta;

export default meta;

type Story = StoryObj;

const noop = () => undefined;

/** Release channels tab with create/manage modal wiring. */
function ReleaseChannelsTabInSitu(): ReactElement {
  const [modalOpen, setModalOpen] = useState(false);
  const [modalMode, setModalMode] = useState<"create" | "manage">("manage");
  const [draft, setDraft] = useState<ReleaseChannelDraft>(devChannelDraft);

  const openForManage = (channel: ReleaseChannel) => {
    setModalMode("manage");
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
              setModalMode("create");
              setDraft({ ...devChannelDraft, name: "", description: "" });
              setModalOpen(true);
            }}
            onManage={openForManage}
          />
        }
      />
      <ReleaseChannelModal
        open={modalOpen}
        mode={modalMode}
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
