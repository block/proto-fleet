import { type ReactNode, useMemo, useState } from "react";

import { RolloutStatus } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import type { ReleaseChannelsApi } from "@/protoFleet/api/useReleaseChannels";
import { pairLabel } from "@/protoFleet/features/settings/components/ReleaseChannels/rolloutStatus";
import StatusChip from "@/protoFleet/features/settings/components/ReleaseChannels/StatusChip";
import Button, { sizes, variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";

interface FirmwareUsageBadgeProps {
  checksum: string;
  filename: string;
  api: Pick<ReleaseChannelsApi, "channels" | "rollouts" | "hasLoaded" | "error">;
  refreshWarning: ReactNode;
  onManageChannel: (channelId: bigint) => void;
}

const FirmwareUsageBadge = ({ checksum, filename, api, refreshWarning, onManageChannel }: FirmwareUsageBadgeProps) => {
  const [open, setOpen] = useState(false);
  // Match payload identity, not mutable metadata or an arbitrary duplicate's file ID.
  // This describes channel references; direct firmware commands can also prevent deletion.
  const channels = useMemo(
    () =>
      checksum
        ? api.channels.flatMap((channel) => {
            const models = new Set([
              ...channel.modelGroups.filter((group) => group.firmwareChecksum === checksum).map(pairLabel),
              ...api.rollouts
                .filter(
                  (rollout) =>
                    rollout.channelId === channel.id &&
                    rollout.status === RolloutStatus.ACTIVE &&
                    rollout.firmwareChecksum === checksum,
                )
                .map(pairLabel),
            ]);
            return models.size ? [{ id: channel.id, name: channel.name, models: [...models].sort() }] : [];
          })
        : [],
    [checksum, api.channels, api.rollouts],
  );

  if (!api.hasLoaded) return null;

  return (
    <>
      {channels.length > 0 ? (
        <button
          type="button"
          aria-label={`View channels using ${filename}`}
          aria-haspopup="dialog"
          className="w-fit cursor-pointer rounded-full outline-none hover:opacity-80 focus-visible:ring-2 focus-visible:ring-core-primary-fill focus-visible:ring-offset-2 focus-visible:ring-offset-surface-base"
          onClick={() => setOpen(true)}
        >
          <StatusChip label="In use" tone="info" />
        </button>
      ) : null}
      {open ? (
        <Modal
          open
          title="Firmware usage"
          description={filename}
          testId="firmware-usage-modal"
          onDismiss={() => setOpen(false)}
          buttons={[{ text: "Done", variant: variants.primary, onClick: () => setOpen(false) }]}
        >
          {refreshWarning}
          {channels.length > 0 ? (
            <>
              <p className="text-300 text-text-primary-70">
                This firmware is assigned to these channels or used by an active update.
              </p>
              <ul className="divide-y divide-border-5">
                {channels.map((channel) => (
                  <li key={channel.id.toString()} className="flex items-center justify-between gap-4 py-4">
                    <div className="min-w-0">
                      <p className="text-emphasis-300 break-words">{channel.name}</p>
                      <p className="text-200 break-words text-text-primary-70">{channel.models.join(", ")}</p>
                    </div>
                    <Button
                      variant={variants.secondary}
                      size={sizes.compact}
                      text="Manage"
                      ariaLabel={`Manage ${channel.name}`}
                      className="shrink-0"
                      onClick={() => {
                        setOpen(false);
                        onManageChannel(channel.id);
                      }}
                    />
                  </li>
                ))}
              </ul>
            </>
          ) : !api.error ? (
            <p className="text-300 text-text-primary-70">No release channels currently use this firmware.</p>
          ) : null}
        </Modal>
      ) : null}
    </>
  );
};

export default FirmwareUsageBadge;
