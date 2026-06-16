import { useCallback, useMemo, useState } from "react";
import SinglePickerField from "./SinglePickerField";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import {
  MAINTENANCE_WINDOW_QUICK_OPTIONS,
  MAINTENANCE_WINDOW_SCOPE_OPTIONS,
  toLocalDatetimeValue,
} from "@/protoFleet/features/notifications/lib/maintenanceWindowOptions";
import { useNotificationsStore } from "@/protoFleet/features/notifications/store/notificationsStore";
import type {
  MaintenanceWindowScope,
  MaintenanceWindowScopeKind,
  MaintenanceWindowWithActive,
} from "@/protoFleet/features/notifications/types";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
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

const AddMaintenanceWindowModal = ({
  open,
  editingMaintenanceWindow,
  prefillRuleId,
  onDismiss,
}: AddMaintenanceWindowModalProps) => {
  const rules = useNotificationsStore((s) => s.rules);
  const createMaintenanceWindow = useNotificationsStore((s) => s.createMaintenanceWindow);
  const updateMaintenanceWindow = useNotificationsStore((s) => s.updateMaintenanceWindow);

  const isEditing = editingMaintenanceWindow != null;

  const [scope, setScope] = useState<MaintenanceWindowScopeKind>("rule");
  const [ruleId, setRuleId] = useState<string | null>(null);
  const [quick, setQuick] = useState<string | null>(DEFAULT_QUICK);
  const [starts, setStarts] = useState("");
  const [ends, setEnds] = useState("");
  const [comment, setComment] = useState("");
  const [errorMsg, setErrorMsg] = useState("");
  const [saving, setSaving] = useState(false);

  const [syncedFor, setSyncedFor] = useState<string | null>(null);
  const syncKey = open ? (editingMaintenanceWindow?.id ?? `__add__${prefillRuleId ?? ""}`) : null;
  if (syncedFor !== syncKey) {
    setSyncedFor(syncKey);
    if (open) {
      if (editingMaintenanceWindow) {
        setScope(editingMaintenanceWindow.scope.kind);
        setRuleId(editingMaintenanceWindow.scope.rule_id);
        setQuick(null);
        setStarts(toLocalDatetimeValue(new Date(editingMaintenanceWindow.starts_at)));
        setEnds(
          editingMaintenanceWindow.ends_at ? toLocalDatetimeValue(new Date(editingMaintenanceWindow.ends_at)) : "",
        );
        setComment(editingMaintenanceWindow.comment);
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

    // Non-rule scopes have no editor here; preserve the stored target so the server's scope validation passes.
    const scopePayload: MaintenanceWindowScope =
      isEditing && editingMaintenanceWindow && editingMaintenanceWindow.scope.kind !== "rule"
        ? editingMaintenanceWindow.scope
        : {
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
      if (isEditing && editingMaintenanceWindow) {
        await updateMaintenanceWindow({ id: editingMaintenanceWindow.id, ...payload });
        pushToast({ message: "Maintenance window updated", status: STATUSES.success });
      } else {
        await createMaintenanceWindow(payload);
        pushToast({ message: "Maintenance window saved", status: STATUSES.success });
      }
      onDismiss();
    } catch (error) {
      pushToast({
        message: getErrorMessage(error, "Failed to save maintenance window"),
        status: STATUSES.error,
      });
      setSaving(false);
    }
  }, [
    starts,
    ends,
    scope,
    ruleId,
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
      title={isEditing ? "Edit maintenance window" : "Add maintenance window"}
      description="Mute alerts during planned work. Suppressed events still record to the activity log so you can audit what would have fired."
      buttons={[
        {
          text: saving ? "Saving…" : "Save maintenance window",
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
            id="maintenance-window-scope"
            label="Maintenance window"
            options={MAINTENANCE_WINDOW_SCOPE_OPTIONS}
            value={scope}
            onChange={(value) => {
              setScope(value as MaintenanceWindowScopeKind);
              clearError();
            }}
          />
          {scope === "rule" ? (
            <SinglePickerField
              id="maintenance-window-rule"
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
          id="maintenance-window-quick"
          label="Quick window"
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
