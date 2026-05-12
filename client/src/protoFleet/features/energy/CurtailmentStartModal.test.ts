import { createElement, type ReactNode } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { formatRestoreEstimate, getRestoreEstimate } from "@/protoFleet/features/energy/curtailmentPreviewHelpers";
import CurtailmentStartModal from "@/protoFleet/features/energy/CurtailmentStartModal";
import {
  defaultCurtailmentFormValues,
  mockPreview,
  storybookCurtailmentFormValues,
} from "@/protoFleet/features/energy/fixtures";
import type { CurtailmentFormValues, CurtailmentPlanPreview } from "@/protoFleet/features/energy/types";

vi.mock("@/shared/components/PageOverlay", () => ({
  __esModule: true,
  default: ({ children }: { children: ReactNode }) => createElement("div", null, children),
}));

interface RenderCurtailmentStartModalOptions {
  onDismiss?: () => void;
  onPreviewCurtailmentPlan?: (values: CurtailmentFormValues) => Promise<CurtailmentPlanPreview>;
  onStartCurtailment?: (values: CurtailmentFormValues) => Promise<unknown>;
  initialValues?: CurtailmentFormValues;
}

beforeEach(() => {
  localStorage.clear();
});

function renderCurtailmentStartModal({
  onDismiss = vi.fn(),
  onPreviewCurtailmentPlan = vi.fn().mockResolvedValue(mockPreview),
  onStartCurtailment = vi.fn().mockResolvedValue(undefined),
  initialValues,
}: RenderCurtailmentStartModalOptions = {}): ReturnType<typeof render> {
  return render(
    createElement(CurtailmentStartModal, {
      open: true,
      onDismiss,
      onPreviewCurtailmentPlan,
      onStartCurtailment,
      initialValues,
    }),
  );
}

describe("CurtailmentStartModal helpers", () => {
  it("estimates total restore duration from selected candidates and restore controls", () => {
    const estimate = getRestoreEstimate({
      selectedCandidateCount: 18,
      restoreBatchSize: "10",
      restoreBatchIntervalSec: "120",
    });

    expect(estimate).toEqual({ batchCount: 2, totalSeconds: 120 });
    if (!estimate) {
      throw new Error("expected a restore estimate");
    }
    expect(formatRestoreEstimate(estimate)).toBe("2m");
  });

  it("does not estimate restore duration when restore controls use server defaults", () => {
    expect(
      getRestoreEstimate({
        selectedCandidateCount: 18,
        restoreBatchSize: "",
        restoreBatchIntervalSec: "120",
      }),
    ).toBeUndefined();
  });
});

describe("CurtailmentStartModal", () => {
  it("prepopulates plans from initial values", () => {
    renderCurtailmentStartModal({
      initialValues: {
        ...defaultCurtailmentFormValues,
        targetKw: "125",
        toleranceKw: "8",
        priority: "emergency",
        minCurtailedDurationSec: "300",
        maxDurationSec: "1800",
        restoreBatchSize: "15",
        restoreBatchIntervalSec: "90",
        reason: "Grid peak event",
      },
    });

    expect(screen.getByLabelText("Target reduction")).toHaveValue(125);
    expect(screen.getByLabelText("Tolerance")).toHaveValue(8);
    expect(screen.getByLabelText("Priority")).toHaveTextContent("Emergency");
    expect(screen.getByLabelText("Min duration")).toHaveValue(300);
    expect(screen.getByLabelText("Max duration")).toHaveValue(1800);
    expect(screen.getByLabelText("Restore batch size")).toHaveValue(15);
    expect(screen.getByLabelText("Restore interval")).toHaveValue(90);
    expect(screen.getByLabelText("Reason")).toHaveValue("Grid peak event");
  });

  it("updates field values locally", () => {
    renderCurtailmentStartModal();

    fireEvent.change(screen.getByLabelText("Target reduction"), { target: { value: "70" } });
    fireEvent.change(screen.getByLabelText("Restore interval"), { target: { value: "45" } });

    expect(screen.getByLabelText("Target reduction")).toHaveValue(70);
    expect(screen.getByLabelText("Restore interval")).toHaveValue(45);
  });

  it("shows the empty preview prompt before the plan is configured", () => {
    renderCurtailmentStartModal();

    expect(screen.getAllByText("Configure your curtailment to see a preview.").length).toBeGreaterThan(0);
  });

  it("does not show required field errors before interaction", async () => {
    renderCurtailmentStartModal();

    expect(screen.queryByText("Required")).not.toBeInTheDocument();
    expect(screen.queryByText("Reason is required")).not.toBeInTheDocument();

    fireEvent.click(screen.getAllByRole("button", { name: /start curtailment/i })[0]);

    await waitFor(() => expect(screen.getByText("Required")).toBeVisible());
    expect(screen.getByText("Reason is required")).toBeVisible();
  });

  it("shows the summary preview", async () => {
    renderCurtailmentStartModal({
      onPreviewCurtailmentPlan: vi.fn().mockResolvedValue({
        ...mockPreview,
        selectedCandidateCount: 19,
        eligibleCandidateCount: 58,
      }),
      initialValues: storybookCurtailmentFormValues,
    });

    await waitFor(() => expect(screen.getByText("Target reduction")).toBeInTheDocument());

    expect(screen.getAllByText("Curtail 19 miners across the fleet immediately").length).toBeGreaterThan(0);
    expect(screen.getAllByText("60.2 of 60.0 kW").length).toBeGreaterThan(0);
    expect(screen.getAllByText("~2 minutes").length).toBeGreaterThan(0);
  });

  it("confirms maintenance inclusion before submitting", async () => {
    const onStartCurtailment = vi.fn().mockResolvedValue(undefined);

    renderCurtailmentStartModal({
      onStartCurtailment,
      initialValues: storybookCurtailmentFormValues,
    });

    fireEvent.click(screen.getByText("Include miners in maintenance"));

    expect(screen.getByTestId("curtailment-maintenance-confirmation")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Force include" }));
    fireEvent.click(screen.getAllByRole("button", { name: /start curtailment/i })[0]);

    await waitFor(() => expect(onStartCurtailment).toHaveBeenCalled());

    expect(onStartCurtailment.mock.calls[0][0]).toEqual(
      expect.objectContaining({
        includeMaintenance: true,
        forceIncludeMaintenance: true,
      }),
    );
  });
});
