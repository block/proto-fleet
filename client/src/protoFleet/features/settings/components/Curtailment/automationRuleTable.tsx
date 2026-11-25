import type { ReactElement } from "react";

import type { AutomationRule, ResponseProfile } from "@/protoFleet/api/curtailmentTypes";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Switch from "@/shared/components/Switch";

// Keeps the trailing "enabled" switch column a fixed width on phones.
export const automationRuleTableClassName = [
  "mb-2 w-full",
  "phone:table-fixed",
  "phone:[&_thead_th:last-child]:w-14",
  "phone:[&_thead_th:last-child>div]:w-14",
  "phone:[&_tbody_td[data-testid=enabled]:last-child>div:first-child]:box-border",
  "phone:[&_tbody_td[data-testid=enabled]:last-child>div:first-child]:flex",
  "phone:[&_tbody_td[data-testid=enabled]:last-child>div:first-child]:justify-end",
  "phone:[&_tbody_td[data-testid=enabled]:last-child>div:first-child]:w-14",
].join(" ");

export const emptyUpdatingRuleIds: ReadonlySet<string> = new Set<string>();

export type AutomationRuleWithDetails = AutomationRule & {
  responseProfileName: string;
  isResponseProfileAutomationReady: boolean;
};

export function withResponseProfileDetails(
  rules: AutomationRule[],
  responseProfiles: ResponseProfile[],
): AutomationRuleWithDetails[] {
  const responseProfilesById = new Map(responseProfiles.map((profile) => [profile.id, profile]));

  return rules.map((rule) => {
    const responseProfile = responseProfilesById.get(rule.responseProfileId);
    return {
      ...rule,
      responseProfileName: responseProfile?.name ?? rule.responseProfileName ?? "Unknown profile",
      isResponseProfileAutomationReady: responseProfile?.isAutomationReady === true,
    };
  });
}

// ruleNoun completes "Edit this … and save the current profile to resume."
export function automationRuleNameColumn(ruleNoun: string, width: string) {
  return {
    allowWrap: true,
    component: (rule: AutomationRuleWithDetails) => (
      <div>
        <span className="block max-w-full truncate text-emphasis-300 text-text-primary">{rule.name}</span>
        {rule.bindingStatus === "rebind_required" ? (
          <div className="text-200 wrap-anywhere whitespace-normal text-text-critical">
            {`Profile changed. Edit this ${ruleNoun} and save the current profile to resume.`}
          </div>
        ) : rule.lastError ? (
          <div className="text-200 wrap-anywhere whitespace-normal text-text-critical">{rule.lastError}</div>
        ) : null}
      </div>
    ),
    width,
  };
}

export function automationRuleEnabledColumn(onToggle: (ruleId: string) => void, updatingRuleIds: ReadonlySet<string>) {
  return {
    component: (rule: AutomationRuleWithDetails) => (
      <div className="flex justify-end" data-interactive>
        <Switch
          checked={rule.enabled}
          setChecked={() => onToggle(rule.id)}
          disabled={updatingRuleIds.has(rule.id) || (!rule.enabled && !rule.isResponseProfileAutomationReady)}
        />
      </div>
    ),
    width: "w-[6%] phone:w-14",
  };
}

type AutomationRuleListEmptyStateOptions = {
  loadError: string | null;
  isLoading: boolean;
  errorTitle: string;
  emptyTitle: string;
  emptyDescription: string;
};

export function automationRuleListEmptyState({
  loadError,
  isLoading,
  errorTitle,
  emptyTitle,
  emptyDescription,
}: AutomationRuleListEmptyStateOptions): ReactElement {
  if (loadError) {
    return <SettingsEmptyState size="section" title={errorTitle} description={loadError} />;
  }
  if (isLoading) {
    return (
      <div className="flex min-h-[220px] w-full items-center justify-center py-14">
        <ProgressCircular indeterminate />
      </div>
    );
  }
  return <SettingsEmptyState size="section" title={emptyTitle} description={emptyDescription} />;
}
