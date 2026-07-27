import { useEffect, useState } from "react";
import { Navigate } from "react-router-dom";
import { updatesClient } from "@/protoFleet/api/clients";
import { ReleaseChannel } from "@/protoFleet/api/generated/updates/v1/updates_pb";
import type { GetUpdateStatusResponse } from "@/protoFleet/api/generated/updates/v1/updates_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import { copyInstallCommand } from "@/protoFleet/features/updates/copyInstallCommand";
import { useAuthErrors, useHasPermission } from "@/protoFleet/store";
import { Copy } from "@/shared/assets/icons";
import Header from "@/shared/components/Header";
import Row from "@/shared/components/Row";
import SegmentedControl from "@/shared/components/SegmentedControl";
import SkeletonBar from "@/shared/components/SkeletonBar";
import { pushToast, STATUSES } from "@/shared/features/toaster";

const SkeletonLoader = <SkeletonBar className="h-[22px] w-24" />;
const UPDATES_PAGE_DESCRIPTION = "View the server version and choose which releases this instance installs.";

const CHANNEL_SEGMENTS = [
  { key: String(ReleaseChannel.STABLE), title: "Stable" },
  { key: String(ReleaseChannel.STABLE_AND_RC), title: "Stable + RC" },
];

const Updates = () => {
  const canUpdateInstance = useHasPermission("instance:update");
  const { handleAuthErrors } = useAuthErrors();
  const [status, setStatus] = useState<GetUpdateStatusResponse | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  // The channel the server has persisted; the source of truth the control
  // reverts to when a save fails.
  const [channel, setChannel] = useState<ReleaseChannel>(ReleaseChannel.UNSPECIFIED);
  const [channelControlResetKey, setChannelControlResetKey] = useState(0);

  useEffect(() => {
    // The RPC is server-gated on instance:update; non-holders are redirected
    // below and must not fire it.
    if (!canUpdateInstance) {
      return;
    }
    updatesClient
      .getUpdateStatus({})
      .then((response) => {
        setStatus(response);
        setChannel(response.channel);
      })
      .catch((err) => {
        handleAuthErrors({
          error: err,
          onError: () => setLoadError(getErrorMessage(err, "Failed to load update status")),
        });
      });
  }, [canUpdateInstance, handleAuthErrors]);

  const handleChannelSelect = (selectedKey: string) => {
    const nextChannel = Number(selectedKey) as ReleaseChannel;
    if (nextChannel === channel) {
      return;
    }
    updatesClient
      .setReleaseChannel({ channel: nextChannel })
      .then(() => {
        setChannel(nextChannel);
        pushToast({
          message: "Release channel updated",
          status: STATUSES.success,
        });
      })
      .catch((err) => {
        handleAuthErrors({
          error: err,
          onError: () => {
            // The SegmentedControl is uncontrolled, so revert by bumping its
            // key: the remount re-reads the still-persisted channel.
            setChannelControlResetKey((key) => key + 1);
            pushToast({
              message: getErrorMessage(err, "Failed to update release channel"),
              status: STATUSES.error,
            });
          },
        });
      });
  };

  // Redirect callers without instance:update away — placed after all
  // hooks to satisfy rules-of-hooks.
  if (!canUpdateInstance) {
    return <Navigate to="/settings/network" replace />;
  }

  const release = status?.updateAvailable ? status.latestEligible : undefined;

  return (
    <div className="flex flex-col gap-6">
      <SettingsPageHeader title="Updates" description={UPDATES_PAGE_DESCRIPTION} />
      {loadError ? (
        <SettingsEmptyState size="section" title="Unable to load update status" description={loadError} />
      ) : (
        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-4 rounded-xl border border-border-5 p-6">
            <Header title="Server version" titleSize="text-heading-200" />
            <div>
              <Row className="flex justify-between" divider>
                <div className="text-300">Current version</div>
                <div className="text-300">{status?.currentVersion ?? SkeletonLoader}</div>
              </Row>
              {release ? (
                <>
                  <Row className="flex justify-between" divider>
                    <div className="text-300">Latest available</div>
                    <div className="flex items-center gap-3">
                      <span className="text-300">{release.version}</span>
                      <a
                        href={release.releaseNotesUrl}
                        target="_blank"
                        rel="noreferrer"
                        className="text-300 text-text-primary-70 underline underline-offset-2 hover:text-text-primary"
                      >
                        Release notes
                      </a>
                    </div>
                  </Row>
                  <Row className="flex items-center justify-between gap-2" divider={false}>
                    <code className="min-w-0 truncate font-mono text-200 text-text-primary-70">
                      {status?.installCommand}
                    </code>
                    <button
                      type="button"
                      onClick={() => copyInstallCommand(status?.installCommand ?? "")}
                      className="flex h-8 shrink-0 items-center gap-2 rounded-lg bg-core-primary-10 px-2 text-200 whitespace-nowrap text-text-primary hover:cursor-pointer hover:bg-core-primary-20"
                    >
                      <Copy width="w-4" />
                      Copy install command
                    </button>
                  </Row>
                </>
              ) : (
                <Row className="flex justify-between" divider={false}>
                  <div className="text-300">{status ? "You're on the latest version" : SkeletonLoader}</div>
                </Row>
              )}
            </div>
          </div>
          <div className="flex flex-col gap-4 rounded-xl border border-border-5 p-6">
            <Header title="Release channel" titleSize="text-heading-200" />
            {status ? (
              <SegmentedControl
                key={`${channel}-${channelControlResetKey}`}
                segments={CHANNEL_SEGMENTS}
                initialSegmentKey={String(channel)}
                onSelect={handleChannelSelect}
              />
            ) : (
              <SkeletonBar className="h-8 w-44" />
            )}
            <p className="text-200 text-text-primary-50">
              Stable + RC also installs release candidates; an RC install cannot downgrade until the next stable
              release.
            </p>
          </div>
        </div>
      )}
    </div>
  );
};

export default Updates;
