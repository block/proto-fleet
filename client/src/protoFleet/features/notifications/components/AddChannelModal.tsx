import { useCallback, useState } from "react";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import {
  type ChannelMutationInput,
  testChannel as testChannelApi,
} from "@/protoFleet/features/notifications/api/notificationsApi";
import type { Channel, ChannelKind, SlackConfig, WebhookConfig } from "@/protoFleet/features/notifications/types";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import SegmentedControl from "@/shared/components/SegmentedControl";
import { pushToast, STATUSES } from "@/shared/features/toaster";

interface AddChannelModalProps {
  open: boolean;
  onDismiss: () => void;
  onCreate: (input: ChannelMutationInput) => Promise<Channel>;
}

const AddChannelModal = ({ open, onDismiss, onCreate }: AddChannelModalProps) => {
  const [kind, setKind] = useState<ChannelKind>("webhook");
  const [name, setName] = useState("");
  const [webhookUrl, setWebhookUrl] = useState("");
  const [bearerHeader, setBearerHeader] = useState("");
  const [slackWebhookUrl, setSlackWebhookUrl] = useState("");

  const [errorMsg, setErrorMsg] = useState("");
  const [saving, setSaving] = useState(false);

  const [prevOpen, setPrevOpen] = useState(open);
  if (prevOpen !== open) {
    setPrevOpen(open);
    if (!open) {
      setKind("webhook");
      setName("");
      setWebhookUrl("");
      setBearerHeader("");
      setSlackWebhookUrl("");
      setErrorMsg("");
      setSaving(false);
    }
  }

  const clearError = () => setErrorMsg("");

  const buildPayload = useCallback((): {
    name: string;
    kind: ChannelKind;
    webhook: WebhookConfig | null;
    slack: SlackConfig | null;
  } | null => {
    const trimmedName = name.trim();
    if (!trimmedName) {
      setErrorMsg("Add a name for this channel");
      return null;
    }
    let webhook: WebhookConfig | null = null;
    let slack: SlackConfig | null = null;
    if (kind === "webhook") {
      const url = webhookUrl.trim();
      if (!url) {
        setErrorMsg("Add a webhook URL");
        return null;
      }
      webhook = { url, bearer_header: bearerHeader.trim() || null };
    } else {
      const url = slackWebhookUrl.trim();
      if (!url) {
        setErrorMsg("Add a Slack webhook URL");
        return null;
      }
      slack = { webhook_url: url };
    }
    return { name: trimmedName, kind, webhook, slack };
  }, [name, kind, webhookUrl, bearerHeader, slackWebhookUrl]);

  const handleSendTest = useCallback(async () => {
    const payload = buildPayload();
    if (!payload) return;
    try {
      const result = await testChannelApi(payload);
      if (result.ok) {
        pushToast({
          message: `Test delivered (HTTP ${result.response_code})`,
          status: STATUSES.success,
        });
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
  }, [buildPayload]);

  const handleSave = useCallback(async () => {
    const payload = buildPayload();
    if (!payload) return;
    setSaving(true);
    try {
      const created: Channel = await onCreate(payload);
      pushToast({ message: `Saved: ${created.name}`, status: STATUSES.success });
      onDismiss();
    } catch (error) {
      pushToast({
        message: getErrorMessage(error, "Failed to save channel"),
        status: STATUSES.error,
      });
      setSaving(false);
    }
  }, [buildPayload, onCreate, onDismiss]);

  const canTest = kind === "webhook" ? webhookUrl.trim().length > 0 : slackWebhookUrl.trim().length > 0;

  return (
    <Modal
      open={open}
      onDismiss={onDismiss}
      title="Add channel"
      description="Pick a destination. Test the channel before saving so you don't ship a dead receiver into the live config."
      buttons={[
        ...(canTest
          ? [
              {
                text: "Send test",
                onClick: () => {
                  void handleSendTest();
                },
                variant: variants.secondary,
                dismissModalOnClick: false,
                className: "animate-[fade-in_.3s_ease-in-out]",
              },
            ]
          : []),
        {
          text: saving ? "Saving…" : "Save channel",
          onClick: () => {
            void handleSave();
          },
          variant: variants.primary,
          dismissModalOnClick: false,
          disabled: saving,
        },
      ]}
      divider={false}
    >
      {errorMsg ? <Callout className="mb-6" intent="danger" prefixIcon={<Alert />} title={errorMsg} /> : null}

      <div className="flex flex-col gap-4">
        <SegmentedControl
          segments={[
            { key: "webhook", title: "Webhook" },
            { key: "slack", title: "Slack" },
          ]}
          initialSegmentKey={kind}
          onSelect={(key) => {
            setKind(key as ChannelKind);
            clearError();
          }}
        />

        <Input
          id="channel-name"
          label="Name"
          initValue={name}
          onChange={(value) => {
            setName(value);
            clearError();
          }}
          autoFocus
        />

        {kind === "webhook" ? (
          <>
            <Input
              id="channel-webhook-url"
              label="URL"
              initValue={webhookUrl}
              onChange={(value) => {
                setWebhookUrl(value);
                clearError();
              }}
            />
            <Input
              id="channel-webhook-bearer"
              label="Bearer header (optional)"
              type="password"
              initValue={bearerHeader}
              onChange={(value) => {
                setBearerHeader(value);
                clearError();
              }}
            />
          </>
        ) : (
          <Input
            id="channel-slack-webhook-url"
            label="Slack webhook URL"
            type="password"
            initValue={slackWebhookUrl}
            onChange={(value) => {
              setSlackWebhookUrl(value);
              clearError();
            }}
          />
        )}

        <p className="pt-2 text-200 text-text-primary-50">
          Verify the destination before saving — Alertmanager doesn't let you test in place.
        </p>
      </div>
    </Modal>
  );
};

export default AddChannelModal;
