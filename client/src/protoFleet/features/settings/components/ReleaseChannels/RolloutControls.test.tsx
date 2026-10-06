import { useState } from "react";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { rolloutBehaviorErrors } from "./behaviorUtils";
import RolloutControls from "./RolloutControls";
import {
  type RolloutBehavior,
  RolloutBehaviorSchema,
  RolloutMethod,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { rolloutBehaviorForRequest } from "@/protoFleet/api/rolloutBehavior";

const reviewedBehavior = () =>
  create(RolloutBehaviorSchema, {
    method: RolloutMethod.BATCHED,
    batchSize: 5,
    reviewAfterEachBatch: true,
    autoContinueOnHealthyTelemetry: true,
    thresholds: { maxHashrateDropPercent: 10, maxNewErrors: 0, minSampleCoveragePercent: 80 },
  });

function renderControls(initial = reviewedBehavior()) {
  const onChange = vi.fn<(behavior: RolloutBehavior) => void>();
  function Controlled() {
    const [behavior, setBehavior] = useState(initial);
    return (
      <RolloutControls
        behavior={behavior}
        onChange={(next) => {
          onChange(next);
          setBehavior(next);
        }}
      />
    );
  }
  render(<Controlled />);
  return () => onChange.mock.lastCall?.[0] ?? initial;
}

afterEach(cleanup);

describe("delegated controller timeout", () => {
  it.each([
    [0, "Never times out"],
    [1, "1 second"],
    [125, "125 seconds"],
  ] as const)("shows the saved %s-second timeout read-only", (controllerTimeoutSeconds, expected) => {
    render(
      <RolloutControls
        behavior={create(RolloutBehaviorSchema, { method: RolloutMethod.DELEGATED, controllerTimeoutSeconds })}
        onChange={vi.fn()}
      />,
    );
    expect(screen.getByText("Controller timeout")).toBeVisible();
    expect(screen.getByText(expected)).toBeVisible();
    expect(screen.queryByRole("textbox", { name: /controller timeout/i })).not.toBeInTheDocument();
    expect(
      screen.getByText(
        controllerTimeoutSeconds === 0
          ? "Updates can wait indefinitely for controller action."
          : "While waiting for the controller, updates pause after this interval without controller action.",
      ),
    ).toBeVisible();
  });

  it("reflects refreshed timeouts and hides retained values for other methods", () => {
    const onChange = vi.fn();
    const behavior = create(RolloutBehaviorSchema, { method: RolloutMethod.DELEGATED, controllerTimeoutSeconds: 125 });
    const { rerender } = render(<RolloutControls behavior={behavior} onChange={onChange} />);
    rerender(
      <RolloutControls
        behavior={create(RolloutBehaviorSchema, { ...behavior, controllerTimeoutSeconds: 0 })}
        onChange={onChange}
      />,
    );
    expect(screen.getByText("Never times out")).toBeVisible();
    expect(screen.queryByText("125 seconds")).not.toBeInTheDocument();
    rerender(
      <RolloutControls
        behavior={create(RolloutBehaviorSchema, { ...behavior, method: RolloutMethod.ALL_AT_ONCE })}
        onChange={onChange}
      />,
    );
    expect(screen.queryByText("Controller timeout")).not.toBeInTheDocument();
    expect(onChange).not.toHaveBeenCalled();
  });
});

describe("sample coverage controls", () => {
  it("shows the saved coverage and preserves fractional edits in the request", () => {
    const current = renderControls();
    const coverage = screen.getByLabelText("Min sample coverage (%)");
    expect(coverage).toHaveValue("80");
    fireEvent.change(coverage, { target: { value: "82.5" } });
    expect(rolloutBehaviorForRequest(current()).thresholds?.minSampleCoveragePercent).toBe(82.5);
    expect(rolloutBehaviorErrors(current())).toEqual({});
  });

  it("makes clearing the coverage use the documented 100% default, not an explicit zero", () => {
    const current = renderControls();
    fireEvent.change(screen.getByLabelText("Min sample coverage (%)"), { target: { value: "" } });
    expect(screen.getByLabelText("Min sample coverage (%)")).toHaveValue("");
    expect(screen.getByText(/Empty means 100%/)).toBeInTheDocument();
    expect(rolloutBehaviorForRequest(current()).thresholds?.minSampleCoveragePercent).toBeUndefined();
    expect(rolloutBehaviorForRequest(current()).thresholds?.maxHashrateDropPercent).toBe(10);
    expect(rolloutBehaviorErrors(current())).toEqual({});
  });

  it.each(["0", "-1", "100.01", "1e999", "85%"])("retains and rejects invalid coverage text %s", (text) => {
    const current = renderControls();
    fireEvent.change(screen.getByLabelText("Min sample coverage (%)"), { target: { value: text } });
    expect(screen.getByLabelText("Min sample coverage (%)")).toHaveValue(text);
    expect(screen.getByText("Enter a number greater than 0 and at most 100.")).toBeInTheDocument();
    expect(current().thresholds?.minSampleCoveragePercent).not.toBeUndefined();
    expect(rolloutBehaviorErrors(current()).minSampleCoveragePercent).toBeDefined();
  });

  it("retains invalid text when auto-continue is off without including it in requests", () => {
    const current = renderControls();
    fireEvent.change(screen.getByLabelText("Min sample coverage (%)"), { target: { value: "80percent" } });
    fireEvent.click(screen.getByLabelText("Auto-continue healthy batches"));
    expect(screen.queryByLabelText("Min sample coverage (%)")).not.toBeInTheDocument();
    expect(rolloutBehaviorErrors(current())).toEqual({});
    expect(rolloutBehaviorForRequest(current()).thresholds).toBeUndefined();
    fireEvent.click(screen.getByLabelText("Auto-continue healthy batches"));
    expect(screen.getByLabelText("Min sample coverage (%)")).toHaveValue("80percent");
    expect(rolloutBehaviorErrors(current()).minSampleCoveragePercent).toBeDefined();
  });

  it("hides coverage for error-only checks and restores the draft for an explicit zero sampled limit", () => {
    const current = renderControls();
    fireEvent.change(screen.getByLabelText("Min sample coverage (%)"), { target: { value: "80percent" } });
    fireEvent.change(screen.getByLabelText("Max hashrate drop (%)"), { target: { value: "" } });
    expect(screen.queryByLabelText("Min sample coverage (%)")).not.toBeInTheDocument();
    expect(rolloutBehaviorErrors(current())).toEqual({});
    expect(rolloutBehaviorForRequest(current()).thresholds?.minSampleCoveragePercent).toBeUndefined();
    expect(rolloutBehaviorForRequest(current()).thresholds?.maxNewErrors).toBe(0);
    fireEvent.change(screen.getByLabelText("Max hashrate drop (%)"), { target: { value: "0" } });
    expect(screen.getByLabelText("Min sample coverage (%)")).toHaveValue("80percent");
    expect(rolloutBehaviorErrors(current()).minSampleCoveragePercent).toBeDefined();
  });
});
