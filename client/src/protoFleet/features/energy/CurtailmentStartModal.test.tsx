import type { ComponentProps } from "react";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import CurtailmentStartModal, {
  type CurtailmentFormValues,
  type CurtailmentPlanPreview,
} from "@/protoFleet/features/energy/CurtailmentStartModal";

vi.mock("@/protoFleet/components/FullScreenTwoPaneModal", () => ({
  default: ({ title, isBusy, buttons, abovePanes, primaryPane, secondaryPane }: any) => (
    <div role="dialog" aria-label={title} data-busy={isBusy ? "true" : "false"}>
      <button type="button" disabled={Boolean(buttons?.[0]?.loading)} onClick={buttons?.[0]?.onClick}>
        {buttons?.[0]?.text}
      </button>
      <div data-testid="above-panes">{abovePanes}</div>
      <div data-testid="primary-pane">{primaryPane}</div>
      <div data-testid="secondary-pane">{secondaryPane}</div>
    </div>
  ),
}));

const configuredValues: Partial<CurtailmentFormValues> = {
  targetKw: "60",
  minDurationSec: "300",
  maxDurationSec: "3600",
  restoreBatchSize: "10",
  restoreIntervalSec: "120",
  reason: "Grid peak - ERCOT 4CP signal",
};

const preview: CurtailmentPlanPreview = {
  selectedMinerCount: 18,
  targetKw: 60,
  estimatedReductionKw: 60.2,
  restoreEstimate: "~2 minutes",
  scopeLabel: "across the fleet",
};

const renderModal = (props: Partial<ComponentProps<typeof CurtailmentStartModal>> = {}) => {
  const onDismiss = vi.fn();
  const onSubmit = vi.fn();

  return {
    onDismiss,
    onSubmit,
    ...render(<CurtailmentStartModal open onDismiss={onDismiss} onSubmit={onSubmit} {...props} />),
  };
};

describe("CurtailmentStartModal", () => {
  it("renders the empty state and disables target selectors until selection wiring is present", () => {
    renderModal();

    expect(screen.getByRole("dialog", { name: "Plan a curtailment" })).toBeInTheDocument();
    expect(screen.getAllByText("Configure your curtailment to see a preview.")).toHaveLength(2);
    expect(screen.getByRole("button", { name: /Racks\s+Select/ })).toBeDisabled();
    expect(screen.getByRole("button", { name: /Groups\s+Select/ })).toBeDisabled();
    expect(screen.getByRole("button", { name: /Miners\s+Select/ })).toBeDisabled();
  });

  it("renders preview and preview error states", () => {
    const { rerender } = renderModal({ initialValues: configuredValues, preview });

    expect(screen.getAllByText("Curtail 18 miners across the fleet immediately")).toHaveLength(2);
    expect(screen.getAllByText("60.2 kW of 60.0 kW")).toHaveLength(2);
    expect(screen.getAllByText("~2 minutes")).toHaveLength(2);

    rerender(
      <CurtailmentStartModal
        open
        onDismiss={vi.fn()}
        onSubmit={vi.fn()}
        initialValues={configuredValues}
        previewError="Preview is unavailable until a valid target reduction is entered."
      />,
    );

    expect(screen.getAllByText("Preview is unavailable until a valid target reduction is entered.")).toHaveLength(2);
  });

  it("submits the current form values without dismissing the modal", async () => {
    const user = userEvent.setup();
    const { onDismiss, onSubmit } = renderModal();

    await user.type(screen.getByLabelText("Target reduction"), "75");
    await user.type(screen.getByLabelText("Reason"), "Grid response");
    await user.click(screen.getByRole("button", { name: "Start curtailment" }));

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        targetKw: "75",
        reason: "Grid response",
        priority: "normal",
      }),
    );
    expect(onDismiss).not.toHaveBeenCalled();
  });

  it("resets form values when reopened with new initial values", async () => {
    const user = userEvent.setup();
    const onDismiss = vi.fn();
    const onSubmit = vi.fn();
    const { rerender } = render(
      <CurtailmentStartModal
        open
        onDismiss={onDismiss}
        onSubmit={onSubmit}
        initialValues={{ targetKw: "10", reason: "Initial reason" }}
      />,
    );

    await user.clear(screen.getByLabelText("Target reduction"));
    await user.type(screen.getByLabelText("Target reduction"), "99");
    expect(screen.getByLabelText("Target reduction")).toHaveValue(99);

    rerender(
      <CurtailmentStartModal
        open={false}
        onDismiss={onDismiss}
        onSubmit={onSubmit}
        initialValues={{ targetKw: "10", reason: "Initial reason" }}
      />,
    );
    rerender(
      <CurtailmentStartModal
        open
        onDismiss={onDismiss}
        onSubmit={onSubmit}
        initialValues={{ targetKw: "25", reason: "Updated reason" }}
      />,
    );

    expect(screen.getByLabelText("Target reduction")).toHaveValue(25);
    expect(screen.getByLabelText("Reason")).toHaveValue("Updated reason");
  });

  it("renders field validation errors with accessible error state", () => {
    renderModal({
      errors: {
        targetKw: "Required",
        reason: "Reason is required",
      },
    });

    expect(screen.getByLabelText("Target reduction")).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByLabelText("Target reduction")).toHaveAttribute(
      "aria-describedby",
      "curtailment-target-kw-error",
    );
    expect(screen.getByText("Required")).toBeInTheDocument();
    expect(screen.getByLabelText("Reason")).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByText("Reason is required")).toBeInTheDocument();
  });
});
