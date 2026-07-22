import { useCallback, useEffect, useMemo, useState } from "react";
import AddChannelModal from "./AddChannelModal";
import ChannelEditableCell from "./ChannelEditableCell";
import ChannelStatusBadge from "./ChannelStatusBadge";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useChannels } from "@/protoFleet/features/alerts/api/useChannels";
import type { Channel } from "@/protoFleet/features/alerts/types";
import { useHasPermission } from "@/protoFleet/store";
import { Checkmark, Lock, Trash } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles, ListAction } from "@/shared/components/List/types";
import { pushToast, STATUSES } from "@/shared/features/toaster";

type ChannelColumns = "name" | "destination" | "status";

const colTitles: ColTitles<ChannelColumns> = {
  name: "Name",
  destination: "Destination",
  status: "Status",
};

const activeCols: ChannelColumns[] = ["name", "destination", "status"];

const formatDestination = (c: Channel) => {
  if (c.kind === "webhook") return c.webhook?.url ?? "";
  return c.has_secret ? "Slack webhook (hidden)" : "";
};

const destinationPlaceholder = (c: Channel) => {
  if (c.kind === "slack") return "https://hooks.slack.com/services/…";
  return "https://hooks…";
};

const ChannelsSection = () => {
  const { channels, refresh, createChannel, updateChannel, testChannel, removeChannel } = useChannels();
  const canManage = useHasPermission("alert:manage");

  const [showAddModal, setShowAddModal] = useState(false);

  useEffect(() => {
    void refresh().catch((error) => {
      pushToast({
        message: getErrorMessage(error, "Failed to load alerts"),
        status: STATUSES.error,
      });
    });
  }, [refresh]);

  const handleSaveName = useCallback(
    async (channel: Channel, next: string) => {
      try {
        await updateChannel({
          id: channel.id,
          name: next,
          kind: channel.kind,
          webhook: channel.webhook,
          slack: channel.slack,
        });
        pushToast({ message: `Renamed: ${next}`, status: STATUSES.success });
      } catch (error) {
        pushToast({
          message: getErrorMessage(error, "Failed to rename channel"),
          status: STATUSES.error,
        });
      }
    },
    [updateChannel],
  );

  const handleSaveDestination = useCallback(
    async (channel: Channel, next: string) => {
      try {
        const base = { id: channel.id, name: channel.name, kind: channel.kind };
        const input =
          channel.kind === "webhook"
            ? { ...base, webhook: { url: next, bearer_header: null }, slack: null }
            : { ...base, webhook: null, slack: { webhook_url: next } };
        await updateChannel(input);
        pushToast({ message: "Destination updated", status: STATUSES.success });
      } catch (error) {
        pushToast({
          message: getErrorMessage(error, "Failed to update destination"),
          status: STATUSES.error,
        });
      }
    },
    [updateChannel],
  );

  const handleTest = useCallback(
    async (channel: Channel) => {
      try {
        const result = await testChannel({
          id: channel.id,
          name: channel.name,
          kind: channel.kind,
          webhook: channel.webhook,
          slack: channel.slack,
        });
        if (result.ok) {
          pushToast({ message: "Test delivery sent", status: STATUSES.success });
        } else {
          pushToast({
            message: `Test failed (HTTP ${result.response_code}): ${result.error || "no detail"}`,
            status: STATUSES.error,
          });
        }
      } catch (error) {
        pushToast({
          message: getErrorMessage(error, "Test delivery failed"),
          status: STATUSES.error,
        });
      }
    },
    [testChannel],
  );

  const handleClearBearer = useCallback(
    async (channel: Channel) => {
      try {
        // Keep the destination (redacted URL maps back to the stored one) and revoke only the bearer.
        await updateChannel({
          id: channel.id,
          name: channel.name,
          kind: channel.kind,
          webhook: { url: channel.webhook?.url ?? "", bearer_header: null, clear_bearer_header: true },
          slack: null,
        });
        pushToast({ message: "Bearer token cleared", status: STATUSES.success });
      } catch (error) {
        pushToast({
          message: getErrorMessage(error, "Failed to clear bearer token"),
          status: STATUSES.error,
        });
      }
    },
    [updateChannel],
  );

  const handleDelete = useCallback(
    async (channel: Channel) => {
      try {
        await removeChannel(channel.id);
        pushToast({ message: `Deleted channel "${channel.name}"`, status: STATUSES.success });
      } catch (error) {
        pushToast({
          message: getErrorMessage(error, "Failed to delete channel"),
          status: STATUSES.error,
        });
      }
    },
    [removeChannel],
  );

  const actions: ListAction<Channel>[] = useMemo(
    () => [
      {
        title: "Test",
        icon: <Checkmark />,
        actionHandler: handleTest,
      },
      {
        title: "Clear bearer token",
        icon: <Lock />,
        actionHandler: handleClearBearer,
        // Only webhook channels carry a bearer, and only when one is stored.
        hidden: (channel) => channel.kind !== "webhook" || !channel.has_secret,
      },
      {
        title: "Delete",
        icon: <Trash />,
        variant: "destructive",
        actionHandler: handleDelete,
      },
    ],
    [handleTest, handleClearBearer, handleDelete],
  );

  const colConfig: ColConfig<Channel, string, ChannelColumns> = useMemo(
    () => ({
      name: {
        component: (channel) => (
          <ChannelEditableCell
            value={channel.name}
            placeholder="Name"
            ariaLabel="name"
            readOnly={!canManage}
            onSave={(next) => {
              void handleSaveName(channel, next);
            }}
          />
        ),
        width: "w-64",
      },
      destination: {
        component: (channel) => (
          <ChannelEditableCell
            value={formatDestination(channel)}
            placeholder={destinationPlaceholder(channel)}
            ariaLabel="destination"
            readOnly={!canManage}
            onSave={(next) => {
              void handleSaveDestination(channel, next);
            }}
          />
        ),
        width: "w-96",
        allowWrap: true,
      },
      status: {
        component: (channel) => <ChannelStatusBadge state={channel.validation_state} />,
        width: "w-40",
      },
    }),
    [handleSaveName, handleSaveDestination, canManage],
  );

  return (
    <section className="flex flex-col gap-4 rounded-xl border border-border-5 p-6">
      <div className="flex items-center justify-between">
        <Header title="Channels" titleSize="text-heading-200" />
        {canManage ? (
          <Button
            variant={variants.secondary}
            size={sizes.compact}
            text="Add channel"
            onClick={() => setShowAddModal(true)}
          />
        ) : null}
      </div>
      <p className="text-300 text-text-primary-50">
        Webhook and Slack destinations for alert delivery. Every rule delivers to every channel unless it has custom
        delivery set in the Rules section above. "Test" sends a synthetic alert directly to the destination.
      </p>

      <List<Channel, string, ChannelColumns>
        items={channels}
        itemKey="id"
        activeCols={activeCols}
        colTitles={colTitles}
        colConfig={colConfig}
        total={channels.length}
        itemName={{ singular: "channel", plural: "channels" }}
        noDataElement={
          <div className="py-10 text-center text-text-primary-50">
            No channels yet — add a webhook or Slack URL to start getting alerts.
          </div>
        }
        actions={canManage ? actions : []}
      />

      <AddChannelModal open={showAddModal} onDismiss={() => setShowAddModal(false)} onCreate={createChannel} />
    </section>
  );
};

export default ChannelsSection;
