import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { CurtailmentFormValues } from "@/protoFleet/features/energy/CurtailmentStartModal";
import { CurtailmentSettingsContent } from "@/protoFleet/features/settings/components/Curtailment";
import type { ResponseProfile } from "@/protoFleet/features/settings/components/Curtailment/types";

// Capture the CurtailmentStartModal initialValues the content derives so we can
// assert the picker-driven scope default without driving the full modal UI.
const { capturedInitialValues } = vi.hoisted(() => ({
  capturedInitialValues: { current: undefined as Partial<CurtailmentFormValues> | undefined },
}));

vi.mock("@/protoFleet/features/energy/CurtailmentStartModal", () => ({
  __esModule: true,
  default: ({ open, initialValues }: { open: boolean; initialValues?: Partial<CurtailmentFormValues> }) => {
    if (open) {
      capturedInitialValues.current = initialValues;
    }
    return open ? <div data-testid="curtailment-start-modal" /> : null;
  },
}));

vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(),
  STATUSES: { error: "error", success: "success" },
}));

const wholeOrgProfile: ResponseProfile = {
  id: "whole-org-profile",
  name: "Whole org profile",
  targetSummary: "500 kW target",
  scope: "Whole fleet",
  selectionStrategy: "Least efficient first",
  restoreBehavior: "Restore immediately",
  deadlineSummary: "Within 15 min",
  formValues: {
    name: "Whole org profile",
    actionType: "fixedKwReduction",
    targetKw: "500",
    deviceIdentifiers: [],
    siteId: "",
    siteName: "",
    selectionStrategy: "leastEfficientFirst",
    restoreBehavior: "automaticImmediateRestore",
    minDurationSec: "",
    maxDurationSec: "900",
    curtailBatchSize: "50",
    curtailBatchIntervalSec: "30",
    restoreBatchSize: "10000",
    restoreIntervalSec: "0",
    responseDeadlineMinutes: "15",
    includeMaintenance: false,
  },
};

describe("Curtailment response-profile site default", () => {
  beforeEach(() => {
    capturedInitialValues.current = undefined;
  });

  it("defaults a new profile to the selected site's scope", () => {
    render(
      <CurtailmentSettingsContent
        responseProfiles={[]}
        sources={[]}
        automationRules={[]}
        initialResponseProfileModalOpen
        defaultSiteScope={{ siteId: "7", siteName: "Site Seven" }}
      />,
    );

    expect(capturedInitialValues.current?.scopeType).toBe("site");
    expect(capturedInitialValues.current?.siteId).toBe("7");
    expect(capturedInitialValues.current?.scopeId).toBe("Site Seven");
  });

  it("keeps the whole-org default when no site is selected", () => {
    render(
      <CurtailmentSettingsContent
        responseProfiles={[]}
        sources={[]}
        automationRules={[]}
        initialResponseProfileModalOpen
      />,
    );

    expect(capturedInitialValues.current?.scopeType).toBe("wholeOrg");
    expect(capturedInitialValues.current?.siteId).toBe("");
  });

  it("does not re-scope an existing whole-org profile when editing with a site selected", () => {
    render(
      <CurtailmentSettingsContent
        responseProfiles={[wholeOrgProfile]}
        sources={[]}
        automationRules={[]}
        defaultSiteScope={{ siteId: "7", siteName: "Site Seven" }}
      />,
    );

    const card = screen.getByText("Whole org profile").closest(".rounded-xl") as HTMLElement;
    fireEvent.click(within(card).getByRole("button", { name: "Edit" }));

    expect(capturedInitialValues.current?.scopeType).toBe("wholeOrg");
    expect(capturedInitialValues.current?.siteId).toBe("");
  });
});
