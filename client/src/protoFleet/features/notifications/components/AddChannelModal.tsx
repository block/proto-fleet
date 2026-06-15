import { useCallback, useState } from "react";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { testChannel as testChannelApi } from "@/protoFleet/features/notifications/api/notificationsApi";
import { useNotificationsStore } from "@/protoFleet/features/notifications/store/notificationsStore";
import type {
  Channel,
  ChannelKind,
  SlackConfig,
  SmtpConfig,
  WebhookConfig,
} from "@/protoFleet/features/notifications/types";
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
}

const AddChannelModal = ({ open, onDismiss }: AddChannelModalProps) => {
  const createChannel = useNotificationsStore((s) => s.createChannel);

  const [kind, setKind] = useState<ChannelKind>("webhook");
  const [name, setName] = useState("");
  const [webhookUrl, setWebhookUrl] = useState("");
  const [bearerHeader, setBearerHeader] = useState("");
  const [slackWebhookUrl, setSlackWebhookUrl] = useState("");
  const [smtpHost, setSmtpHost] = useState("");
  const [smtpPort, setSmtpPort] = useState("");
  const [smtpUsername, setSmtpUsername] = useState("");
  const [smtpFrom, setSmtpFrom] = useState("");
  const [smtpTo, setSmtpTo] = useState("");
  const [smtpPassword, setSmtpPassword] = useState("");

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
      setSmtpHost("");
      setSmtpPort("");
      setSmtpUsername("");
      setSmtpFrom("");
      setSmtpTo("");
      setSmtpPassword("");
      setErrorMsg("");
      setSaving(false);
    }
  }

  const clearError = () => setErrorMsg("");

  // Returns null and sets errorMsg when validation fails.
  const buildPayload = useCallback((): {
    name: string;
    kind: ChannelKind;
    webhook: WebhookConfig | null;
    smtp: SmtpConfig | null;
    slack: SlackConfig | null;
  } | null => {
    const trimmedName = name.trim();
    if (!trimmedName) {
      setErrorMsg("Add a name for this channel");
      return null;
    }
    let webhook: WebhookConfig | null = null;
    let smtp: SmtpConfig | null = null;
    let slack: SlackConfig | null = null;
    if (kind === "webhook") {
      const url = webhookUrl.trim();
      if (!url) {
        setErrorMsg("Add a webhook URL");
        return null;
      }
      webhook = { url, bearer_header: bearerHeader.trim() || null };
    } else if (kind === "slack") {
      const url = slackWebhookUrl.trim();
      if (!url) {
        setErrorMsg("Add a Slack webhook URL");
        return null;
      }
      slack = { webhook_url: url };
    } else {
      const host = smtpHost.trim();
      const port = parseInt(smtpPort, 10) || 587;
      const to = smtpTo
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean);
      if (!host || to.length === 0) {
        setErrorMsg("Need a host and at least one To address");
        return null;
      }
      smtp = {
        host,
        port,
        username: smtpUsername.trim(),
        from: smtpFrom.trim(),
        to,
        password: smtpPassword || undefined,
      };
    }
    return { name: trimmedName, kind, webhook, smtp, slack };
  }, [
    name,
    kind,
    webhookUrl,
    bearerHeader,
    slackWebhookUrl,
    smtpHost,
    smtpPort,
    smtpUsername,
    smtpFrom,
    smtpTo,
    smtpPassword,
  ]);

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
      const created: Channel = await createChannel(payload);
      pushToast({ message: `Saved: ${created.name}`, status: STATUSES.success });
      onDismiss();
    } catch (error) {
      pushToast({
        message: getErrorMessage(error, "Failed to save channel"),
        status: STATUSES.error,
      });
      setSaving(false);
    }
  }, [buildPayload, createChannel, onDismiss]);

  // Testable once a URL (webhook/Slack) or a To address (SMTP) is present.
  const canTest =
    kind === "webhook"
      ? webhookUrl.trim().length > 0
      : kind === "slack"
        ? slackWebhookUrl.trim().length > 0
        : smtpTo.trim().length > 0;

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
            { key: "smtp", title: "SMTP (email)" },
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
              initValue={bearerHeader}
              onChange={(value) => {
                setBearerHeader(value);
                clearError();
              }}
            />
          </>
        ) : kind === "slack" ? (
          <Input
            id="channel-slack-webhook-url"
            label="Slack webhook URL"
            initValue={slackWebhookUrl}
            onChange={(value) => {
              setSlackWebhookUrl(value);
              clearError();
            }}
          />
        ) : (
          <>
            <div className="grid grid-cols-[1fr_120px] gap-4">
              <Input
                id="channel-smtp-host"
                label="Host"
                initValue={smtpHost}
                onChange={(value) => {
                  setSmtpHost(value);
                  clearError();
                }}
              />
              <Input
                id="channel-smtp-port"
                label="Port"
                initValue={smtpPort}
                onChange={(value) => {
                  setSmtpPort(value);
                  clearError();
                }}
              />
            </div>
            <Input
              id="channel-smtp-username"
              label="Username"
              initValue={smtpUsername}
              onChange={(value) => {
                setSmtpUsername(value);
                clearError();
              }}
            />
            <Input
              id="channel-smtp-password"
              label="Password (optional)"
              initValue={smtpPassword}
              onChange={(value) => {
                setSmtpPassword(value);
                clearError();
              }}
            />
            <Input
              id="channel-smtp-from"
              label="From"
              initValue={smtpFrom}
              onChange={(value) => {
                setSmtpFrom(value);
                clearError();
              }}
            />
            <Input
              id="channel-smtp-to"
              label="To (comma-separated)"
              initValue={smtpTo}
              onChange={(value) => {
                setSmtpTo(value);
                clearError();
              }}
            />
          </>
        )}

        <p className="pt-2 text-200 text-text-primary-50">
          Verify the destination before saving — Alertmanager doesn't let you test in place.
        </p>
      </div>
    </Modal>
  );
};

export default AddChannelModal;
