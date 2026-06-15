import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";

import { curtailmentClient } from "@/protoFleet/api/clients";
import {
  type CurtailmentAutomationRule as ApiCurtailmentAutomationRule,
  CreateCurtailmentAutomationRuleRequestSchema,
  CurtailmentAutomationTriggerType,
  DeleteCurtailmentAutomationRuleRequestSchema,
  ListCurtailmentAutomationRulesRequestSchema,
  SetCurtailmentAutomationRuleEnabledRequestSchema,
  UpdateCurtailmentAutomationRuleRequestSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import { assertNotAborted, isAbortError, toError } from "@/protoFleet/api/requestErrors";
import type {
  AutomationRule,
  AutomationRuleFormValues,
} from "@/protoFleet/features/settings/components/Curtailment/types";
import { useAuthErrors } from "@/protoFleet/store";

export type UseCurtailmentAutomationRulesResult = {
  automationRules: AutomationRule[];
  isLoading: boolean;
  isCreating: boolean;
  updatingRuleIds: ReadonlySet<string>;
  loadError: string | null;
  createError: string | null;
  listAutomationRules: (signal?: AbortSignal) => Promise<AutomationRule[]>;
  createAutomationRule: (values: AutomationRuleFormValues) => Promise<AutomationRule>;
  updateAutomationRule: (ruleId: string, values: AutomationRuleFormValues) => Promise<AutomationRule>;
  setAutomationRuleEnabled: (ruleId: string, enabled: boolean) => Promise<AutomationRule>;
  deleteAutomationRule: (ruleId: string) => Promise<void>;
};

function getAutomationConditionSummary(sourceName: string): string {
  return sourceName ? `${sourceName} grid signal changes to 0` : "Grid signal changes to 0";
}

function mapApiAutomationRule(rule: ApiCurtailmentAutomationRule, priority: number): AutomationRule {
  return {
    id: rule.ruleId.toString(),
    priority,
    name: rule.ruleName,
    conditionType: "mqttTriggerTargetOff",
    conditionSummary: getAutomationConditionSummary(rule.mqttSourceName),
    sourceId: rule.mqttSourceId.toString(),
    responseProfileId: rule.responseProfileId.toString(),
    responseProfileName: rule.responseProfileName,
    enabled: rule.enabled,
  };
}

function assignRulePriorities(rules: AutomationRule[]): AutomationRule[] {
  return rules.map((rule, index) => ({ ...rule, priority: index + 1 }));
}

function parsePositiveBigIntId(value: string, label: string): bigint {
  const trimmedValue = value.trim();
  if (!/^[1-9]\d*$/.test(trimmedValue)) {
    throw new Error(`Select a valid ${label}.`);
  }

  return BigInt(trimmedValue);
}

function buildAutomationRulePayload(values: AutomationRuleFormValues) {
  return {
    ruleName: values.name.trim(),
    triggerType: CurtailmentAutomationTriggerType.MQTT,
    mqttSourceId: parsePositiveBigIntId(values.sourceId, "source"),
    responseProfileId: parsePositiveBigIntId(values.responseProfileId, "response profile"),
  };
}

export default function useCurtailmentAutomationRules(enabled = true): UseCurtailmentAutomationRulesResult {
  const { handleAuthErrors } = useAuthErrors();
  const [apiRules, setApiRules] = useState<ApiCurtailmentAutomationRule[]>([]);
  const [isLoading, setIsLoading] = useState(enabled);
  const [isCreating, setIsCreating] = useState(false);
  const [updatingRuleIds, setUpdatingRuleIds] = useState<Set<string>>(() => new Set());
  const [loadError, setLoadError] = useState<string | null>(null);
  const [createError, setCreateError] = useState<string | null>(null);
  const hasLoadedRulesRef = useRef(false);

  const automationRules = useMemo(
    () => apiRules.map((rule, index) => mapApiAutomationRule(rule, index + 1)),
    [apiRules],
  );

  const handleFailure = useCallback(
    (error: unknown, fallbackMessage: string): Error => {
      const resolvedError = toError(error, fallbackMessage);
      handleAuthErrors({ error });
      return resolvedError;
    },
    [handleAuthErrors],
  );

  const listAutomationRules = useCallback(
    async (signal?: AbortSignal): Promise<AutomationRule[]> => {
      const shouldShowLoading = !hasLoadedRulesRef.current;
      if (shouldShowLoading) {
        setIsLoading(true);
      }

      try {
        assertNotAborted(signal);
        const response = await curtailmentClient.listCurtailmentAutomationRules(
          create(ListCurtailmentAutomationRulesRequestSchema, {}),
          signal ? { signal } : undefined,
        );
        assertNotAborted(signal);

        setApiRules(response.rules);
        hasLoadedRulesRef.current = true;
        setLoadError(null);
        return response.rules.map((rule, index) => mapApiAutomationRule(rule, index + 1));
      } catch (error) {
        if (isAbortError(error, signal)) {
          throw error;
        }

        const resolvedError = handleFailure(error, "Failed to load automation rules.");
        setLoadError(resolvedError.message);
        throw resolvedError;
      } finally {
        if (shouldShowLoading) {
          setIsLoading(false);
        }
      }
    },
    [handleFailure],
  );

  useEffect(() => {
    if (!enabled) {
      return;
    }

    const abortController = new AbortController();
    // eslint-disable-next-line react-hooks/set-state-in-effect -- initial fetch on mount; setState inside async fetch is the external-sync pattern
    void listAutomationRules(abortController.signal).catch(() => {});

    return () => {
      abortController.abort();
    };
  }, [enabled, listAutomationRules]);

  const createAutomationRule = useCallback(
    async (values: AutomationRuleFormValues): Promise<AutomationRule> => {
      setIsCreating(true);
      setCreateError(null);

      try {
        const response = await curtailmentClient.createCurtailmentAutomationRule(
          create(CreateCurtailmentAutomationRuleRequestSchema, {
            ...buildAutomationRulePayload(values),
            enabled: true,
          }),
        );
        if (!response.rule) {
          throw new Error("Created automation rule response was missing a rule.");
        }

        const createdRule = response.rule;
        setApiRules((currentRules) => [
          ...currentRules.filter((currentRule) => currentRule.ruleId !== createdRule.ruleId),
          createdRule,
        ]);
        return mapApiAutomationRule(createdRule, apiRules.length + 1);
      } catch (error) {
        const resolvedError = handleFailure(error, "Failed to create automation rule.");
        setCreateError(resolvedError.message);
        throw resolvedError;
      } finally {
        setIsCreating(false);
      }
    },
    [apiRules.length, handleFailure],
  );

  const updateAutomationRule = useCallback(
    async (ruleId: string, values: AutomationRuleFormValues): Promise<AutomationRule> => {
      setUpdatingRuleIds((currentIds) => new Set(currentIds).add(ruleId));

      try {
        const response = await curtailmentClient.updateCurtailmentAutomationRule(
          create(UpdateCurtailmentAutomationRuleRequestSchema, {
            ruleId: parsePositiveBigIntId(ruleId, "automation rule"),
            ...buildAutomationRulePayload(values),
          }),
        );
        if (!response.rule) {
          throw new Error("Updated automation rule response was missing a rule.");
        }

        const updatedRule = response.rule;
        setApiRules((currentRules) =>
          currentRules.map((currentRule) => (currentRule.ruleId === updatedRule.ruleId ? updatedRule : currentRule)),
        );
        const priority = automationRules.find((rule) => rule.id === ruleId)?.priority ?? 0;
        return mapApiAutomationRule(updatedRule, priority);
      } catch (error) {
        throw handleFailure(error, "Failed to update automation rule.");
      } finally {
        setUpdatingRuleIds((currentIds) => {
          const nextIds = new Set(currentIds);
          nextIds.delete(ruleId);
          return nextIds;
        });
      }
    },
    [automationRules, handleFailure],
  );

  const setAutomationRuleEnabled = useCallback(
    async (ruleId: string, enabled: boolean): Promise<AutomationRule> => {
      setUpdatingRuleIds((currentIds) => new Set(currentIds).add(ruleId));

      try {
        const response = await curtailmentClient.setCurtailmentAutomationRuleEnabled(
          create(SetCurtailmentAutomationRuleEnabledRequestSchema, {
            ruleId: parsePositiveBigIntId(ruleId, "automation rule"),
            enabled,
          }),
        );
        if (!response.rule) {
          throw new Error("Updated automation rule response was missing a rule.");
        }

        const updatedRule = response.rule;
        setApiRules((currentRules) =>
          currentRules.map((currentRule) => (currentRule.ruleId === updatedRule.ruleId ? updatedRule : currentRule)),
        );
        const priority = automationRules.find((rule) => rule.id === ruleId)?.priority ?? 0;
        return mapApiAutomationRule(updatedRule, priority);
      } catch (error) {
        throw handleFailure(error, "Failed to update automation rule.");
      } finally {
        setUpdatingRuleIds((currentIds) => {
          const nextIds = new Set(currentIds);
          nextIds.delete(ruleId);
          return nextIds;
        });
      }
    },
    [automationRules, handleFailure],
  );

  const deleteAutomationRule = useCallback(
    async (ruleId: string): Promise<void> => {
      setUpdatingRuleIds((currentIds) => new Set(currentIds).add(ruleId));

      try {
        await curtailmentClient.deleteCurtailmentAutomationRule(
          create(DeleteCurtailmentAutomationRuleRequestSchema, {
            ruleId: parsePositiveBigIntId(ruleId, "automation rule"),
          }),
        );
        setApiRules((currentRules) => currentRules.filter((currentRule) => currentRule.ruleId.toString() !== ruleId));
      } catch (error) {
        throw handleFailure(error, "Failed to delete automation rule.");
      } finally {
        setUpdatingRuleIds((currentIds) => {
          const nextIds = new Set(currentIds);
          nextIds.delete(ruleId);
          return nextIds;
        });
      }
    },
    [handleFailure],
  );

  return useMemo(
    () => ({
      automationRules: assignRulePriorities(automationRules),
      isLoading: enabled ? isLoading : false,
      isCreating,
      updatingRuleIds,
      loadError,
      createError,
      listAutomationRules,
      createAutomationRule,
      updateAutomationRule,
      setAutomationRuleEnabled,
      deleteAutomationRule,
    }),
    [
      automationRules,
      enabled,
      isLoading,
      isCreating,
      updatingRuleIds,
      loadError,
      createError,
      listAutomationRules,
      createAutomationRule,
      updateAutomationRule,
      setAutomationRuleEnabled,
      deleteAutomationRule,
    ],
  );
}
