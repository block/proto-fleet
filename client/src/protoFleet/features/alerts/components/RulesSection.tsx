import { useCallback, useMemo, useState } from "react";
import AddMaintenanceWindowModal from "./AddMaintenanceWindowModal";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAlertsContext } from "@/protoFleet/features/alerts/api/AlertsContext";
import { isMaintenanceWindowActive } from "@/protoFleet/features/alerts/api/useAlerts";
import { useNow } from "@/protoFleet/features/alerts/lib/useNow";
import type { Rule } from "@/protoFleet/features/alerts/types";
import { useHasPermission } from "@/protoFleet/store";
import { Pause, Play, Stop } from "@/shared/assets/icons";
import Header from "@/shared/components/Header";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles, ListAction } from "@/shared/components/List/types";
import { pushToast, STATUSES } from "@/shared/features/toaster";

type RuleColumns = "name" | "when" | "severity";

const colTitles: ColTitles<RuleColumns> = {
  name: "Name",
  when: "When",
  severity: "Severity",
};

const activeCols: RuleColumns[] = ["name", "when", "severity"];

const formatRuleCondition = (rule: Rule): string => {
  if (rule.summary) return rule.summary;
  if (rule.duration_seconds > 0) return `fires after ${rule.duration_seconds}s`;
  return "fires on first matching evaluation";
};

const RulesSection = () => {
  const { rules, maintenanceWindows, pauseRule, resumeRule, removeMaintenanceWindow } = useAlertsContext();
  const canManage = useHasPermission("alert:manage");

  const [maintenanceWindowPrefillRuleId, setMaintenanceWindowPrefillRuleId] = useState<string | null>(null);
  const [showMaintenanceWindowModal, setShowMaintenanceWindowModal] = useState(false);

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

  const actions: ListAction<Rule>[] = useMemo(
    () => [
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
    ],
    [handleTogglePause, handleMaintenanceWindowOrLift, activeMaintenanceWindowIdsByRule],
  );

  const colConfig: ColConfig<Rule, string, RuleColumns> = useMemo(
    () => ({
      name: {
        component: (rule) => (
          <span className="flex items-center gap-2">
            <span className="text-emphasis-300 text-text-primary">{rule.name}</span>
            {!rule.enabled ? (
              <span className="rounded bg-surface-5 px-2 py-0.5 text-200 text-text-primary-50">Paused</span>
            ) : null}
          </span>
        ),
        width: "w-64",
      },
      when: {
        component: (rule) => <span className="text-text-primary-50">{formatRuleCondition(rule)}</span>,
        width: "w-[480px]",
        allowWrap: true,
      },
      severity: {
        component: (rule) => <span className="text-text-primary-50">{rule.severity || "—"}</span>,
        width: "w-24",
      },
    }),
    [],
  );

  return (
    <section className="flex flex-col gap-4 rounded-xl border border-border-5 p-6">
      <Header title="Rules" titleSize="text-heading-200" />
      <p className="text-300 text-text-primary-50">
        Provisioned conditions that decide when an alert fires. The rule set is managed by ops — pause one to silence it
        indefinitely, or attach a maintenance window to mute it for a finite period.
      </p>

      <List<Rule, string, RuleColumns>
        items={sortedRules}
        itemKey="id"
        activeCols={activeCols}
        colTitles={colTitles}
        colConfig={colConfig}
        total={sortedRules.length}
        itemName={{ singular: "rule", plural: "rules" }}
        noDataElement={
          <div className="py-10 text-center text-text-primary-50">
            No rules provisioned yet — ask your operator to deploy the Grafana alert-rule bundle.
          </div>
        }
        actions={canManage ? actions : []}
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
    </section>
  );
};

export default RulesSection;
