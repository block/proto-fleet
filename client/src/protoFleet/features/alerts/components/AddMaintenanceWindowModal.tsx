import { useCallback, useMemo, useState } from "react";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAlertsContext } from "@/protoFleet/features/alerts/api/AlertsContext";
import { useChannelSelection } from "@/protoFleet/features/alerts/api/useChannelSelection";
import SinglePickerField from "@/protoFleet/features/alerts/components/SinglePickerField";
import {
  MAINTENANCE_WINDOW_QUICK_OPTIONS,
  MAX_MAINTENANCE_WINDOW_DURATION_MS,
  toLocalDatetimeValue,
} from "@/protoFleet/features/alerts/lib/maintenanceWindowOptions";
import type { MaintenanceWindowWithActive } from "@/protoFleet/features/alerts/types";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Checkbox from "@/shared/components/Checkbox";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import SegmentedControl from "@/shared/components/SegmentedControl";
import { pushToast, STATUSES } from "@/shared/features/toaster";

interface AddMaintenanceWindowModalProps {
  open: boolean;
  editingMaintenanceWindow: MaintenanceWindowWithActive | null;
  prefillRuleId?: string | null;
  onDismiss: () => void;
}

const DEFAULT_QUICK = "4h";

const computeEndsFromQuick = (quick: string): Date => {
  const meta = MAINTENANCE_WINDOW_QUICK_OPTIONS.find((q) => q.id === quick);
  const hours = meta?.hours ?? 4;
  return new Date(Date.now() + hours * 3600 * 1000);
};

type TargetMode = "all" | "selected";

interface TargetOption {
  id: string;
  label: string;
  sublabel?: string;
}

interface TargetPickerProps {
  segments: { key: TargetMode; title: string }[];
  mode: TargetMode;
  onModeChange: (mode: TargetMode) => void;
  allHint: string;
  options: TargetOption[];
  emptyMessage: string;
  selectedIds: Set<string>;
  onToggle: (id: string) => void;
}

// All-or-selected target picker, DeliveryPicker's shape minus the routing modes.
// Hosts must key it per editing session: the SegmentedControl is uncontrolled.
const TargetPicker = ({
  segments,
  mode,
  onModeChange,
  allHint,
  options,
  emptyMessage,
  selectedIds,
  onToggle,
}: TargetPickerProps) => (
  <div className="flex flex-col gap-4">
    <SegmentedControl
      segments={segments}
      initialSegmentKey={mode}
      onSelect={(key) => onModeChange(key as TargetMode)}
    />

    {mode === "selected" ? (
      <div className="flex max-h-56 flex-col gap-2 overflow-y-auto">
        {options.map((option) => (
          <label
            key={option.id}
            className="flex cursor-pointer items-center gap-3 rounded-lg border border-border-5 p-3"
          >
            <Checkbox checked={selectedIds.has(option.id)} onChange={() => onToggle(option.id)} />
            <span className="flex min-w-0 flex-col">
              <span className="truncate text-text-primary">{option.label}</span>
              {option.sublabel ? (
                <span className="truncate text-200 text-text-primary-70">{option.sublabel}</span>
              ) : null}
            </span>
          </label>
        ))}
        {options.length === 0 ? <p className="py-4 text-center text-text-primary-50">{emptyMessage}</p> : null}
      </div>
    ) : (
      <p className="text-200 text-text-primary-50">{allHint}</p>
    )}
  </div>
);

const AddMaintenanceWindowModal = ({
  open,
  editingMaintenanceWindow,
  prefillRuleId,
  onDismiss,
}: AddMaintenanceWindowModalProps) => {
  const { rules, createMaintenanceWindow, updateMaintenanceWindow } = useAlertsContext();

  const isEditing = editingMaintenanceWindow != null;

  const [ruleMode, setRuleMode] = useState<TargetMode>("all");
  const [selectedRuleIds, setSelectedRuleIds] = useState<Set<string>>(new Set());
  const [channelMode, setChannelMode] = useState<TargetMode>("all");
  const [quick, setQuick] = useState<string | null>(DEFAULT_QUICK);
  const [starts, setStarts] = useState("");
  const [ends, setEnds] = useState("");
  const [comment, setComment] = useState("");
  const [errorMsg, setErrorMsg] = useState("");
  const [saving, setSaving] = useState(false);
  const [confirmedAllAlertingMute, setConfirmedAllAlertingMute] = useState(false);
  // Channels only render in selected mode, so the session fetches them lazily then.
  const {
    channels,
    channelsLoaded,
    selectedIds: liveChannelIds,
    toggleChannel: toggleChannelSelection,
    reset: resetChannelSelection,
  } = useChannelSelection(open && channelMode === "selected");

  const [syncedFor, setSyncedFor] = useState<string | null>(null);
  const syncKey = open ? (editingMaintenanceWindow?.id ?? `__add__${prefillRuleId ?? ""}`) : null;
  if (syncedFor !== syncKey) {
    setSyncedFor(syncKey);
    if (open) {
      if (editingMaintenanceWindow) {
        setRuleMode(editingMaintenanceWindow.rule_ids.length > 0 ? "selected" : "all");
        setSelectedRuleIds(new Set(editingMaintenanceWindow.rule_ids));
        setChannelMode(editingMaintenanceWindow.channel_ids.length > 0 ? "selected" : "all");
        resetChannelSelection(editingMaintenanceWindow.channel_ids);
        setQuick(null);
        setStarts(toLocalDatetimeValue(new Date(editingMaintenanceWindow.starts_at)));
        setEnds(
          editingMaintenanceWindow.ends_at ? toLocalDatetimeValue(new Date(editingMaintenanceWindow.ends_at)) : "",
        );
        setComment(editingMaintenanceWindow.comment);
        setErrorMsg("");
        setConfirmedAllAlertingMute(false);
      } else {
        const now = new Date();
        const end = computeEndsFromQuick(DEFAULT_QUICK);
        setRuleMode(prefillRuleId ? "selected" : "all");
        setSelectedRuleIds(new Set(prefillRuleId ? [prefillRuleId] : []));
        setChannelMode("all");
        resetChannelSelection([]);
        setQuick(DEFAULT_QUICK);
        setStarts(toLocalDatetimeValue(now));
        setEnds(toLocalDatetimeValue(end));
        setComment("");
        setErrorMsg("");
        setConfirmedAllAlertingMute(false);
      }
      setSaving(false);
    }
  }

  const clearError = () => setErrorMsg("");

  const ruleOptions = useMemo<TargetOption[]>(() => rules.map((r) => ({ id: r.id, label: r.name })), [rules]);
  const channelOptions = useMemo<TargetOption[]>(
    () => channels.map((c) => ({ id: c.id, label: c.name, sublabel: c.kind === "slack" ? "Slack" : "Webhook" })),
    [channels],
  );

  // Drop selections whose rule no longer exists (like the channel selection's live filter): the
  // server rejects unknown ids, and a stale id would render no checkbox, so it could never be
  // deselected.
  const liveRuleIds = useMemo(() => {
    const live = new Set(rules.map((r) => r.id));
    return new Set([...selectedRuleIds].filter((id) => live.has(id)));
  }, [selectedRuleIds, rules]);

  const toggleRule = (id: string) => {
    setSelectedRuleIds((current) => {
      const next = new Set(current);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
    clearError();
  };
  const toggleChannel = (id: string) => {
    toggleChannelSelection(id);
    clearError();
  };

  const handleQuickChange = useCallback((next: string) => {
    setQuick(next);
    const now = new Date();
    const end = computeEndsFromQuick(next);
    setStarts(toLocalDatetimeValue(now));
    setEnds(toLocalDatetimeValue(end));
    clearError();
  }, []);

  // Hand-editing a datetime drops the quick-window preset to "Custom".
  const handleStartsChange = (value: string) => {
    setStarts(value);
    setQuick(null);
    clearError();
  };
  const handleEndsChange = (value: string) => {
    setEnds(value);
    setQuick(null);
    clearError();
  };

  const handleSave = useCallback(async () => {
    if (!starts || !ends) {
      setErrorMsg("Pick a start and end time");
      return;
    }
    if (new Date(ends) <= new Date(starts)) {
      setErrorMsg("End must be after start");
      return;
    }
    if (new Date(ends).getTime() - new Date(starts).getTime() > MAX_MAINTENANCE_WINDOW_DURATION_MS) {
      setErrorMsg("Quiet periods cannot exceed 30 days");
      return;
    }
    if (ruleMode === "all" && channelMode === "all" && !confirmedAllAlertingMute) {
      setErrorMsg("Confirm that no alerts will be delivered during this quiet period");
      return;
    }
    if (ruleMode === "selected" && liveRuleIds.size === 0) {
      setErrorMsg("Pick at least one alert, or use All alerts");
      return;
    }
    if (channelMode === "selected" && liveChannelIds.size === 0) {
      setErrorMsg("Pick at least one destination, or use All destinations");
      return;
    }

    const payload = {
      rule_ids: ruleMode === "selected" ? [...liveRuleIds] : [],
      channel_ids: channelMode === "selected" ? [...liveChannelIds] : [],
      starts_at: new Date(starts).toISOString(),
      ends_at: new Date(ends).toISOString(),
      comment: comment.trim(),
    };

    setSaving(true);
    try {
      if (isEditing && editingMaintenanceWindow) {
        await updateMaintenanceWindow({ id: editingMaintenanceWindow.id, ...payload });
        pushToast({ message: "Quiet period updated", status: STATUSES.success });
      } else {
        await createMaintenanceWindow(payload);
        pushToast({ message: "Quiet period saved", status: STATUSES.success });
      }
      onDismiss();
    } catch (error) {
      pushToast({
        message: getErrorMessage(error, "Failed to save quiet period"),
        status: STATUSES.error,
      });
      setSaving(false);
    }
  }, [
    starts,
    ends,
    ruleMode,
    liveRuleIds,
    channelMode,
    liveChannelIds,
    confirmedAllAlertingMute,
    comment,
    isEditing,
    editingMaintenanceWindow,
    createMaintenanceWindow,
    updateMaintenanceWindow,
    onDismiss,
  ]);

  return (
    <Modal
      open={open}
      onDismiss={onDismiss}
      title={isEditing ? "Edit quiet period" : "Add quiet period"}
      description="Mute alert delivery during planned work. Alerts still show up in history."
      buttons={[
        {
          text: saving ? "Saving…" : "Save quiet period",
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
        <TargetPicker
          key={`rules-${syncKey ?? ""}`}
          segments={[
            { key: "all", title: "All alerts" },
            { key: "selected", title: "Selected alerts" },
          ]}
          mode={ruleMode}
          onModeChange={(mode) => {
            setRuleMode(mode);
            setConfirmedAllAlertingMute(false);
            clearError();
          }}
          allHint="Every alert rule is muted, including rules added later."
          options={ruleOptions}
          emptyMessage="No rules provisioned yet."
          selectedIds={liveRuleIds}
          onToggle={toggleRule}
        />

        <TargetPicker
          key={`channels-${syncKey ?? ""}`}
          segments={[
            { key: "all", title: "All destinations" },
            { key: "selected", title: "Selected destinations" },
          ]}
          mode={channelMode}
          onModeChange={(mode) => {
            setChannelMode(mode);
            setConfirmedAllAlertingMute(false);
            clearError();
          }}
          allHint="Delivery is muted on every destination, including destinations added later."
          options={channelOptions}
          emptyMessage={
            channelsLoaded
              ? "No destinations yet — add one in the Destinations section first."
              : "Loading destinations…"
          }
          selectedIds={liveChannelIds}
          onToggle={toggleChannel}
        />

        {ruleMode === "all" && channelMode === "all" ? (
          <div className="flex flex-col gap-3">
            <Callout
              intent="warning"
              prefixIcon={<Alert />}
              title="This mutes all alerting"
              subtitle="No alert reaches any destination while this quiet period is active."
            />
            <label className="flex cursor-pointer items-center gap-3 text-300 text-text-primary">
              <Checkbox
                checked={confirmedAllAlertingMute}
                onChange={(event) => {
                  setConfirmedAllAlertingMute(event.target.checked);
                  clearError();
                }}
              />
              I understand that no alerts will be delivered during this quiet period.
            </label>
          </div>
        ) : null}

        <SinglePickerField
          id="maintenance-window-quick"
          label="Quick period"
          options={MAINTENANCE_WINDOW_QUICK_OPTIONS}
          value={quick}
          placeholder="Custom"
          onChange={handleQuickChange}
        />

        <div className="grid grid-cols-2 gap-4">
          <Input
            id="maintenance-window-starts"
            label="Starts"
            type="datetime-local"
            initValue={starts}
            onChange={handleStartsChange}
          />
          <Input
            id="maintenance-window-ends"
            label="Ends"
            type="datetime-local"
            initValue={ends}
            onChange={handleEndsChange}
          />
        </div>

        <Input
          id="maintenance-window-comment"
          label="Reason"
          initValue={comment}
          onChange={(value) => {
            setComment(value);
            clearError();
          }}
        />
      </div>
    </Modal>
  );
};

export default AddMaintenanceWindowModal;
