import { useCallback, useMemo, useState } from "react";
import SinglePickerField from "./SinglePickerField";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import {
  SILENCE_QUICK_OPTIONS,
  SILENCE_SCOPE_OPTIONS,
  toLocalDatetimeValue,
} from "@/protoFleet/features/notifications/lib/silenceOptions";
import { useNotificationsStore } from "@/protoFleet/features/notifications/store/notificationsStore";
import type { SilenceScope, SilenceScopeKind, SilenceWithActive } from "@/protoFleet/features/notifications/types";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import { pushToast, STATUSES } from "@/shared/features/toaster";

interface AddSilenceModalProps {
  open: boolean;
  editingSilence: SilenceWithActive | null;
  prefillRuleId?: string | null;
  onDismiss: () => void;
}

const DEFAULT_QUICK = "4h";

const computeEndsFromQuick = (quick: string): Date => {
  const meta = SILENCE_QUICK_OPTIONS.find((q) => q.id === quick);
  const hours = meta?.hours ?? 4;
  return new Date(Date.now() + hours * 3600 * 1000);
};

const AddSilenceModal = ({ open, editingSilence, prefillRuleId, onDismiss }: AddSilenceModalProps) => {
  const rules = useNotificationsStore((s) => s.rules);
  const createSilence = useNotificationsStore((s) => s.createSilence);
  const updateSilence = useNotificationsStore((s) => s.updateSilence);

  const isEditing = editingSilence != null;

  const [scope, setScope] = useState<SilenceScopeKind>("rule");
  const [ruleId, setRuleId] = useState<string | null>(null);
  const [quick, setQuick] = useState<string | null>(DEFAULT_QUICK);
  const [starts, setStarts] = useState("");
  const [ends, setEnds] = useState("");
  const [comment, setComment] = useState("");
  const [errorMsg, setErrorMsg] = useState("");
  const [saving, setSaving] = useState(false);

  const [syncedFor, setSyncedFor] = useState<string | null>(null);
  const syncKey = open ? (editingSilence?.id ?? `__add__${prefillRuleId ?? ""}`) : null;
  if (syncedFor !== syncKey) {
    setSyncedFor(syncKey);
    if (open) {
      if (editingSilence) {
        setScope(editingSilence.scope.kind);
        setRuleId(editingSilence.scope.rule_id);
        setQuick(null); // Editing: no quick window preset, show explicit datetimes.
        setStarts(toLocalDatetimeValue(new Date(editingSilence.starts_at)));
        setEnds(editingSilence.ends_at ? toLocalDatetimeValue(new Date(editingSilence.ends_at)) : "");
        setComment(editingSilence.comment);
        setErrorMsg("");
      } else {
        const now = new Date();
        const end = computeEndsFromQuick(DEFAULT_QUICK);
        setScope("rule");
        setRuleId(prefillRuleId ?? rules[0]?.id ?? null);
        setQuick(DEFAULT_QUICK);
        setStarts(toLocalDatetimeValue(now));
        setEnds(toLocalDatetimeValue(end));
        setComment("");
        setErrorMsg("");
      }
      setSaving(false);
    }
  }

  const clearError = () => setErrorMsg("");

  const ruleOptions = useMemo(() => rules.map((r) => ({ id: r.id, label: r.name })), [rules]);

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
    if (scope === "rule" && !ruleId) {
      setErrorMsg("Pick a rule");
      return;
    }

    const scopePayload: SilenceScope = {
      kind: scope,
      rule_id: scope === "rule" ? ruleId : null,
      group_id: null,
      site_id: null,
      device_ids: [],
    };

    const payload = {
      scope: scopePayload,
      starts_at: new Date(starts).toISOString(),
      ends_at: new Date(ends).toISOString(),
      comment: comment.trim(),
    };

    setSaving(true);
    try {
      if (isEditing && editingSilence) {
        await updateSilence({ id: editingSilence.id, ...payload });
        pushToast({ message: "Silence updated", status: STATUSES.success });
      } else {
        await createSilence(payload);
        pushToast({ message: "Silence saved", status: STATUSES.success });
      }
      onDismiss();
    } catch (error) {
      pushToast({
        message: getErrorMessage(error, "Failed to save silence"),
        status: STATUSES.error,
      });
      setSaving(false);
    }
  }, [starts, ends, scope, ruleId, comment, isEditing, editingSilence, createSilence, updateSilence, onDismiss]);

  return (
    <Modal
      open={open}
      onDismiss={onDismiss}
      title={isEditing ? "Edit silence" : "Add silence"}
      description="Mute alerts during planned work. Silenced events still record to the activity log so you can audit what would have fired."
      buttons={[
        {
          text: saving ? "Saving…" : "Save silence",
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
        <div className="grid grid-cols-2 gap-4">
          <SinglePickerField
            id="silence-scope"
            label="Silence"
            options={SILENCE_SCOPE_OPTIONS}
            value={scope}
            onChange={(value) => {
              setScope(value as SilenceScopeKind);
              clearError();
            }}
          />
          {scope === "rule" ? (
            <SinglePickerField
              id="silence-rule"
              label="Rule"
              options={ruleOptions}
              value={ruleId}
              placeholder="Pick a rule"
              emptyMessage="No rules provisioned yet."
              onChange={(value) => {
                setRuleId(value);
                clearError();
              }}
            />
          ) : null}
        </div>

        <SinglePickerField
          id="silence-quick"
          label="Quick window"
          options={SILENCE_QUICK_OPTIONS}
          value={quick}
          placeholder="Custom"
          onChange={handleQuickChange}
        />

        <div className="grid grid-cols-2 gap-4">
          <Input
            id="silence-starts"
            label="Starts"
            type="datetime-local"
            initValue={starts}
            onChange={handleStartsChange}
          />
          <Input id="silence-ends" label="Ends" type="datetime-local" initValue={ends} onChange={handleEndsChange} />
        </div>

        <Input
          id="silence-comment"
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

export default AddSilenceModal;
