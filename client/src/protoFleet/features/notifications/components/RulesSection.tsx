import { useCallback, useMemo, useState } from "react";
import AddSilenceModal from "./AddSilenceModal";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useNotificationsStore } from "@/protoFleet/features/notifications/store/notificationsStore";
import type { Rule } from "@/protoFleet/features/notifications/types";
import { Pause, Play, Stop } from "@/shared/assets/icons";
import Header from "@/shared/components/Header";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles, ListAction } from "@/shared/components/List/types";
import { pushToast, STATUSES } from "@/shared/features/toaster";

// Rules are read-only descriptors of the provisioned Grafana alert
// rule set. The Add Rule, Edit Rule, and Delete Rule affordances
// from the prototype are deliberately absent — operators can only
// pause / resume the rules that ship with the deploy, or attach a
// silence to one. New rules require a YAML change + redeploy.

type RuleColumns = "name" | "when" | "severity";

const colTitles: ColTitles<RuleColumns> = {
  name: "Name",
  when: "When",
  severity: "Severity",
};

const activeCols: RuleColumns[] = ["name", "when", "severity"];

// formatRuleCondition pulls the user-facing description from the rule
// metadata Grafana stamps on each rule's annotations + labels. The
// prototype reconstructed this from a user-authored threshold; we
// don't have one (rules are not user-editable), so we lean on the
// annotation Grafana renders in its own UI.
const formatRuleCondition = (rule: Rule): string => {
  if (rule.summary) return rule.summary;
  if (rule.duration_seconds > 0) return `fires after ${rule.duration_seconds}s`;
  return "fires on first matching evaluation";
};

const RulesSection = () => {
  const rules = useNotificationsStore((s) => s.rules);
  const silences = useNotificationsStore((s) => s.silences);
  const pauseRule = useNotificationsStore((s) => s.pauseRule);
  const resumeRule = useNotificationsStore((s) => s.resumeRule);
  const removeSilence = useNotificationsStore((s) => s.removeSilence);

  const [silencePrefillRuleId, setSilencePrefillRuleId] = useState<string | null>(null);
  const [showSilenceModal, setShowSilenceModal] = useState(false);

  // Map ruleId → active silence (if any) so the kebab can flip
  // Silence ⇄ Lift silence. `active` is precomputed by the store
  // when silences are fetched so we don't have to call Date.now()
  // during render (lint blocks that).
  const activeSilenceByRule = useMemo(() => {
    const map = new Map<string, string>();
    silences.forEach((sil) => {
      if (sil.active && sil.scope.kind === "rule" && sil.scope.rule_id) {
        map.set(sil.scope.rule_id, sil.id);
      }
    });
    return map;
  }, [silences]);

  // Enabled rules first, paused at the bottom. Within each group,
  // sort by group + name so the order is stable across reloads.
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

  const handleSilenceOrLift = useCallback(
    async (rule: Rule) => {
      const activeSilenceId = activeSilenceByRule.get(rule.id);
      if (activeSilenceId) {
        try {
          await removeSilence(activeSilenceId);
          pushToast({ message: "Silence lifted", status: STATUSES.success });
        } catch (error) {
          pushToast({
            message: getErrorMessage(error, "Failed to lift silence"),
            status: STATUSES.error,
          });
        }
      } else {
        setSilencePrefillRuleId(rule.id);
        setShowSilenceModal(true);
      }
    },
    [activeSilenceByRule, removeSilence],
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
        title: (rule) => (activeSilenceByRule.has(rule.id) ? "Lift silence" : "Silence"),
        icon: <Stop />,
        actionHandler: (rule) => {
          void handleSilenceOrLift(rule);
        },
      },
    ],
    [handleTogglePause, handleSilenceOrLift, activeSilenceByRule],
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
        width: "w-96",
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
        Provisioned conditions that decide when a notification fires. The rule set is managed by ops — pause one to
        silence it indefinitely, or attach a silence to mute it for a finite maintenance window.
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
        actions={actions}
      />

      <AddSilenceModal
        open={showSilenceModal}
        editingSilence={null}
        prefillRuleId={silencePrefillRuleId}
        onDismiss={() => {
          setShowSilenceModal(false);
          setSilencePrefillRuleId(null);
        }}
      />
    </section>
  );
};

export default RulesSection;
