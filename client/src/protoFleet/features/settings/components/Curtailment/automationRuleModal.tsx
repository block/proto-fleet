import { type ReactElement, useMemo } from "react";

import type { ResponseProfile } from "@/protoFleet/api/curtailmentTypes";
import { SectionDependencyMessage } from "@/protoFleet/features/settings/components/Curtailment/CurtailmentSection";
import Select, { type SelectOption } from "@/shared/components/Select";

export const automationRuleModalSectionLabelClassName =
  "text-200 font-semibold tracking-[0.08em] text-text-primary-50 uppercase";

export function AutomationRuleSaveError({ message }: { message: string | null }): ReactElement | null {
  if (!message) {
    return null;
  }
  return <div className="rounded-lg bg-intent-critical-10 px-4 py-3 text-300 text-text-critical">{message}</div>;
}

type AutomationRuleResponseProfileFieldProps = {
  id: string;
  testId: string;
  responseProfiles: ResponseProfile[];
  value: string;
  onChange: (responseProfileId: string, responseProfileRevision: string) => void;
  error?: string;
  isLoading: boolean;
  loadError: string | null;
};

export function AutomationRuleResponseProfileField({
  id,
  testId,
  responseProfiles,
  value,
  onChange,
  error,
  isLoading,
  loadError,
}: AutomationRuleResponseProfileFieldProps): ReactElement {
  const options = useMemo<SelectOption[]>(
    () => responseProfiles.map((profile) => ({ value: profile.id, label: profile.name })),
    [responseProfiles],
  );

  return (
    <div>
      <Select
        id={id}
        label="Response profile"
        options={options}
        value={value}
        onChange={(responseProfileId) =>
          onChange(
            responseProfileId,
            responseProfiles.find((profile) => profile.id === responseProfileId)?.revision ?? "",
          )
        }
        disabled={isLoading || options.length === 0}
        error={error}
        testId={testId}
        forceBelow
      />
      {loadError ? <SectionDependencyMessage>Unable to load response profiles.</SectionDependencyMessage> : null}
      {!isLoading && !loadError && options.length === 0 ? (
        <SectionDependencyMessage>No response profiles available.</SectionDependencyMessage>
      ) : null}
    </div>
  );
}
