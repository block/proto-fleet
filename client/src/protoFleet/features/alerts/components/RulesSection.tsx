import { useCallback, useMemo, useState } from "react";
import AddMaintenanceWindowModal from "./AddMaintenanceWindowModal";
import AddRuleModal from "./AddRuleModal";
import StatusDot from "./StatusDot";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAlertsContext } from "@/protoFleet/features/alerts/api/AlertsContext";
import { isMaintenanceWindowActive } from "@/protoFleet/features/alerts/api/useAlerts";
import { useNow } from "@/protoFleet/features/alerts/lib/useNow";
import type { Rule } from "@/protoFleet/features/alerts/types";
import { useHasPermission } from "@/protoFleet/store";
import { Edit, Pause, Play, Stop, Trash } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles, ListAction } from "@/shared/components/List/types";
import { pushToast, STATUSES } from "@/shared/features/toaster";

type RuleColumns = "name" | "condition" | "status";

const colTitles: ColTitles<RuleColumns> = {
  name: "Name",
  condition: "Condition",
  status: "Status",
};

const activeCols: RuleColumns[] = ["name", "condition", "status"];

// Borderless cells with a right-aligned action kebab, per the alerts design.
const rulesTableClassName = "mb-6 [&_td]:!border-x-0 [&_th]:!border-x-0 [&_td[data-testid='action']>div]:!ml-auto";

const formatRuleCondition = (rule: Rule): string => {
  if (rule.summary) return rule.summary;
  if (rule.duration_seconds > 0) return `fires after ${rule.duration_seconds}s`;
  return "fires on first matching evaluation";
};

const RulesSection = () => {
  const { rules, maintenanceWindows, pauseRule, resumeRule, removeRule, removeMaintenanceWindow } = useAlertsContext();
  const canManage = useHasPermission("alert:manage");
  const canReadMiners = useHasPermission("miner:read");
  // Rule create/edit additionally require org-wide miner:read server-side
  // (rules fan per-device alerts out); pause/window/delete stay on alert:manage.
  const canWriteRules = canManage && canReadMiners;

  const [maintenanceWindowPrefillRuleId, setMaintenanceWindowPrefillRuleId] = useState<string | null>(null);
  const [showMaintenanceWindowModal, setShowMaintenanceWindowModal] = useState(false);
  const [showRuleModal, setShowRuleModal] = useState(false);
  const [editingRule, setEditingRule] = useState<Rule | null>(null);

  const now = useNow();
  const activeMaintenanceWindowIdsByRule = useMemo(() => {
    // Track every active window per rule, not just the last one, so lifting a rule clears all of them.
    const map = new Map<string, string[]>();
    maintenanceWindows.forEach((sil) => {
      if (isMaintenanceWindowActive(sil, now) && sil.scope.kind === "rule" && sil.scope.rule_id) {
        const ids = map.get(sil.scope.rule_id) ?? [];
        ids.push(sil.id);
        map.set(sil.scope.rule_id, ids);
      }
    });
    return map;
  }, [maintenanceWindows, now]);

  const sortedRules = useMemo(
    () =>
      rules
        .slice()
        .sort(
          (a, b) =>
            Number(!a.enabled) - Number(!b.enabled) || a.group.localeCompare(b.group) || a.name.localeCompare(b.name),
        ),
    [rules],
  );

  const handleTogglePause = useCallback(
    async (rule: Rule) => {
      try {
        if (rule.enabled) {
          await pauseRule(rule.id);
          pushToast({ message: `Paused: ${rule.name}`, status: STATUSES.success });
        } else {
          await resumeRule(rule.id);
          pushToast({ message: `Resumed: ${rule.name}`, status: STATUSES.success });
        }
      } catch (error) {
        pushToast({
          message: getErrorMessage(error, "Failed to update rule"),
          status: STATUSES.error,
        });
      }
    },
    [pauseRule, resumeRule],
  );

  const handleMaintenanceWindowOrLift = useCallback(
    async (rule: Rule) => {
      const activeIds = activeMaintenanceWindowIdsByRule.get(rule.id) ?? [];
      if (activeIds.length > 0) {
        try {
          // Lift every active window for the rule so it isn't left muted by an overlapping one.
          await Promise.all(activeIds.map((id) => removeMaintenanceWindow(id)));
          pushToast({
            message: activeIds.length > 1 ? "Maintenance windows lifted" : "Maintenance window lifted",
            status: STATUSES.success,
          });
        } catch (error) {
          pushToast({
            message: getErrorMessage(error, "Failed to lift maintenance window"),
            status: STATUSES.error,
          });
        }
      } else {
        setMaintenanceWindowPrefillRuleId(rule.id);
        setShowMaintenanceWindowModal(true);
      }
    },
    [activeMaintenanceWindowIdsByRule, removeMaintenanceWindow],
  );

  const handleDelete = useCallback(
    async (rule: Rule) => {
      try {
        await removeRule(rule.id);
        pushToast({ message: `Deleted: ${rule.name}`, status: STATUSES.success });
      } catch (error) {
        pushToast({
          message: getErrorMessage(error, "Failed to delete rule"),
          status: STATUSES.error,
        });
      }
    },
    [removeRule],
  );

  const actions: ListAction<Rule>[] = useMemo(
    () => [
      {
        title: "Edit",
        icon: <Edit />,
        // Without the stored config the modal can't prefill the real trigger,
        // and saving would silently rewrite the rule as an offline check.
        hidden: (rule) => !canWriteRules || rule.origin !== "user" || !rule.config,
        actionHandler: (rule) => {
          setEditingRule(rule);
          setShowRuleModal(true);
        },
      },
      {
        title: (rule) => (rule.enabled ? "Pause" : "Resume"),
        icon: (rule) => (rule.enabled ? <Pause /> : <Play />),
        actionHandler: (rule) => {
          void handleTogglePause(rule);
        },
      },
      {
        title: (rule) =>
          activeMaintenanceWindowIdsByRule.has(rule.id) ? "Lift maintenance window" : "Maintenance window",
        icon: <Stop />,
        actionHandler: (rule) => {
          void handleMaintenanceWindowOrLift(rule);
        },
      },
      {
        title: "Delete",
        icon: <Trash />,
        variant: "destructive",
        hidden: (rule) => rule.origin !== "user",
        actionHandler: (rule) => {
          void handleDelete(rule);
        },
      },
    ],
    [handleTogglePause, handleMaintenanceWindowOrLift, handleDelete, activeMaintenanceWindowIdsByRule, canWriteRules],
  );

  const colConfig: ColConfig<Rule, string, RuleColumns> = useMemo(
    () => ({
      name: {
        component: (rule) => (
          <div className="flex min-w-0 flex-col gap-1">
            <span className="truncate text-emphasis-300 text-text-primary">{rule.name}</span>
            <span className="truncate text-200 text-text-primary-70">
              {rule.origin === "user" ? "Custom rule" : "Default rule"}
            </span>
          </div>
        ),
        width: "w-80",
      },
      condition: {
        component: (rule) => (
          <div className="flex min-w-0 flex-col gap-1">
            <span className="truncate text-text-primary">{formatRuleCondition(rule)}</span>
            <span className="truncate text-200 text-text-primary-70">{rule.severity || "—"}</span>
          </div>
        ),
        width: "w-96",
      },
      status: {
        component: (rule) => {
          if (!rule.enabled) {
            return <StatusDot dotClass="bg-text-primary-30">Paused</StatusDot>;
          }
          // An active maintenance window suppresses the rule even while enabled.
          if (activeMaintenanceWindowIdsByRule.has(rule.id)) {
            return <StatusDot dotClass="bg-intent-warning-fill">Muted</StatusDot>;
          }
          return <StatusDot dotClass="bg-intent-success-fill">Active</StatusDot>;
        },
        width: "w-80",
      },
    }),
    [activeMaintenanceWindowIdsByRule],
  );

  return (
    <section className="flex flex-col gap-4 rounded-xl border border-border-5 p-6">
      <div className="flex flex-col gap-1">
        <div className="flex items-center justify-between">
          <Header title="Rules" titleSize="text-heading-200" />
          {canWriteRules ? (
            <Button
              variant={variants.secondary}
              size={sizes.compact}
              text="Add rule"
              onClick={() => {
                setEditingRule(null);
                setShowRuleModal(true);
              }}
            />
          ) : null}
        </div>
        <p className="text-300 text-text-primary-50">
          Conditions that decide when an alert fires. Add your own rule on a fleet metric, or work with the provisioned
          defaults — pause one to silence it indefinitely, or attach a maintenance window to mute it for a finite
          period.
        </p>
      </div>

      <List<Rule, string, RuleColumns>
        items={sortedRules}
        itemKey="id"
        activeCols={activeCols}
        colTitles={colTitles}
        colConfig={colConfig}
        hideTotal
        noDataElement={
          <div className="py-10 text-center text-text-primary-50">
            {canWriteRules
              ? "No rules yet — click Add rule to set one up."
              : "No rules yet — ask an alert manager to add one."}
          </div>
        }
        actions={canManage ? actions : []}
        applyColumnWidthsToCells
        tableClassName={rulesTableClassName}
      />

      <AddMaintenanceWindowModal
        open={showMaintenanceWindowModal}
        editingMaintenanceWindow={null}
        prefillRuleId={maintenanceWindowPrefillRuleId}
        onDismiss={() => {
          setShowMaintenanceWindowModal(false);
          setMaintenanceWindowPrefillRuleId(null);
        }}
      />

      <AddRuleModal
        open={showRuleModal}
        editingRule={editingRule}
        onDismiss={() => {
          setShowRuleModal(false);
          setEditingRule(null);
        }}
      />
    </section>
  );
};

export default RulesSection;
