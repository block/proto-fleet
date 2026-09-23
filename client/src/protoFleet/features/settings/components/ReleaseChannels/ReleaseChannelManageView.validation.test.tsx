import { useState } from "react";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { closeChannelSettings, manageViewProps, openChannelSettings } from "./__tests__/helpers";
import ReleaseChannelManageView from "./ReleaseChannelManageView";
import RolloutControls from "./RolloutControls";
import {
  ReleaseChannelModelGroupSchema,
  ReleaseChannelSchema,
  RolloutBehaviorSchema,
  RolloutMethod,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

vi.mock("./ScopeEditor", () => ({ default: () => null }));
vi.mock("@/shared/features/toaster", () => ({ pushToast: vi.fn(), STATUSES: { success: "success", error: "error" } }));

const healthyBehavior = () =>
  create(RolloutBehaviorSchema, {
    method: RolloutMethod.BATCHED,
    batchSize: 10,
    pilotSize: 1,
    reviewAfterEachBatch: true,
    autoContinueOnHealthyTelemetry: true,
    stabilizationSeconds: 60,
    thresholds: {
      maxHashrateDropPercent: 10,
      maxEfficiencyIncreasePercent: 10,
      maxTemperatureIncreaseCelsius: 10,
      maxNewErrors: 10,
    },
  });

function renderView(behavior = healthyBehavior()) {
  const props = manageViewProps();
  render(
    <ReleaseChannelManageView
      {...props}
      channel={{
        ...create(ReleaseChannelSchema, { id: 1n, name: "Production", behavior }),
        modelGroups: [
          create(ReleaseChannelModelGroupSchema, {
            manufacturer: "Proto",
            model: "Rig",
            firmwareChecksum: "a".repeat(64),
            firmwareVersion: "1.0",
            firmwareTargetManufacturer: "Proto",
            firmwareTargetModel: "Rig",
            assignmentGeneration: 1n,
          }),
        ],
      }}
    />,
  );
  openChannelSettings();
  fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Renamed production" } });
  return { onSave: props.onSave, onApply: props.onApply };
}

describe("release channel numeric safeguards", () => {
  beforeEach(() =>
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(10, 10, 120, 40)),
  );
  afterEach(() => vi.restoreAllMocks());

  test.each([
    ["Max hashrate drop (%)", "-1"],
    ["Max hashrate drop (%)", "101"],
    ["Max hashrate drop (%)", "10junk"],
    ["Max hashrate drop (%)", "1e"],
    ["Max hashrate drop (%)", "Infinity"],
    ["Max hashrate drop (%)", "1e999"],
    ["Max efficiency increase (%)", "-1"],
    ["Max efficiency increase (%)", "Infinity"],
    ["Max temp increase (°C)", "-1"],
    ["Max temp increase (°C)", "not a number"],
    ["Max errors", "-1"],
    ["Max errors", "1.5"],
    ["Max errors", "2147483648"],
    ["Max errors", "2junk"],
    ["Batch size (miners)", "1.5"],
    ["Batch size (miners)", "2147483648"],
    ["Max miners offline at once (0 for no limit)", "-1"],
    ["Max miners offline at once (0 for no limit)", "1.5"],
    ["Max miners offline at once (0 for no limit)", "2147483648"],
    ["Wait for telemetry (minutes)", "-1"],
    ["Wait for telemetry (minutes)", "0.001"],
    ["Wait for telemetry (minutes)", "1e-18"],
    ["Wait for telemetry (minutes)", "35791395"],
  ])("retains invalid %s=%s, blocks Save, and recovers after correction", async (label, value) => {
    const { onSave } = renderView();
    const input = screen.getByLabelText(label);
    fireEvent.change(input, { target: { value } });
    expect(input).toHaveValue(value);
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.click(screen.getByTestId("save-channel"));
    expect(onSave).not.toHaveBeenCalled();
    fireEvent.change(input, { target: { value: "1" } });
    expect(input).not.toHaveAttribute("aria-invalid");
    await act(async () => fireEvent.click(screen.getByTestId("save-channel")));
    expect(onSave).toHaveBeenCalledOnce();
  });

  test.each([
    ["Max hashrate drop (%)", "maxHashrateDropPercent"],
    ["Max efficiency increase (%)", "maxEfficiencyIncreasePercent"],
    ["Max temp increase (°C)", "maxTemperatureIncreaseCelsius"],
    ["Max errors", "maxNewErrors"],
  ] as const)("distinguishes intentionally empty %s from an explicit zero", async (label, field) => {
    const { onSave } = renderView();
    const input = screen.getByLabelText(label);
    fireEvent.change(input, { target: { value: "" } });
    expect(input).not.toHaveAttribute("aria-invalid");
    await act(async () => fireEvent.click(screen.getByTestId("save-channel")));
    expect(onSave.mock.calls[0][0].behavior.thresholds?.[field]).toBeUndefined();
    fireEvent.change(input, { target: { value: "0" } });
    expect(input).not.toHaveAttribute("aria-invalid");
    await act(async () => fireEvent.click(screen.getByTestId("save-channel")));
    expect(onSave.mock.calls[1][0].behavior.thresholds?.[field]).toBe(0);
  });

  test("keeps inactive invalid thresholds available for correction without passing NaN to Save", async () => {
    const { onSave } = renderView();
    fireEvent.change(screen.getByLabelText("Max hashrate drop (%)"), { target: { value: "10junk" } });
    fireEvent.click(screen.getByLabelText("Auto-continue healthy batches"));
    expect(screen.getByTestId("save-channel")).toBeEnabled();
    await act(async () => fireEvent.click(screen.getByTestId("save-channel")));
    expect(onSave.mock.calls[0][0].behavior.thresholds).toBeUndefined();
    fireEvent.click(screen.getByLabelText("Auto-continue healthy batches"));
    expect(screen.getByLabelText("Max hashrate drop (%)")).toHaveValue("10junk");
    expect(screen.getByLabelText("Max hashrate drop (%)")).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.click(screen.getByTestId("rollout-method"));
    fireEvent.click(screen.getByRole("option", { name: /^Single batch/ }));
    await act(async () => fireEvent.click(screen.getByTestId("save-channel")));
    expect(onSave.mock.calls[1][0].behavior.thresholds).toBeUndefined();
    fireEvent.click(screen.getByTestId("rollout-method"));
    fireEvent.click(screen.getByRole("option", { name: /^Multiple batches/ }));
    expect(screen.getByLabelText("Max hashrate drop (%)")).toHaveValue("10junk");
    expect(screen.getByTestId("save-channel")).toBeDisabled();
  });

  test("allows Apply to use saved behavior while an unsaved safeguard is invalid", async () => {
    const { onSave, onApply } = renderView();
    fireEvent.change(screen.getByLabelText("Max errors"), { target: { value: "-1" } });
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    closeChannelSettings();
    fireEvent.click(screen.getByTestId("channel-firmware-select-Rig"));
    fireEvent.click(screen.getByRole("option", { name: "No firmware" }));
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    expect(screen.getByTestId("apply-firmware-dialog")).toHaveTextContent("Unsaved channel changes");
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Clear assignments" })));
    expect(onApply).toHaveBeenCalledOnce();
    expect(onSave).not.toHaveBeenCalled();
  });

  test.each([
    [RolloutMethod.BATCHED, "Batch size (miners)"],
    [RolloutMethod.PILOT_THEN_CONTINUE, "Pilot batch size (miners)"],
  ] as const)("suppresses numeric plan readouts for invalid method %s sizing", (method, label) => {
    function Controls() {
      const [behavior, setBehavior] = useState(create(RolloutBehaviorSchema, { method, batchSize: 10, pilotSize: 1 }));
      return <RolloutControls behavior={behavior} onChange={setBehavior} inScopeCount={20} />;
    }
    render(<Controls />);
    fireEvent.change(screen.getByLabelText(label), { target: { value: "bad" } });
    expect(screen.getByLabelText(label)).toHaveAttribute("aria-invalid", "true");
    expect(screen.queryByText(/NaN/)).not.toBeInTheDocument();
    expect(screen.queryByText(/across 20 miners|then 19 remaining/)).not.toBeInTheDocument();
  });

  test("preserves stored sub-minute seconds on unrelated saves and accepts whole-second minute conversions", async () => {
    const behavior = healthyBehavior();
    behavior.stabilizationSeconds = 31;
    behavior.waitBetweenBatchesSeconds = 1;
    const { onSave } = renderView(behavior);
    expect(screen.getByLabelText("Wait for telemetry (minutes)")).toHaveValue(String(31 / 60));
    await act(async () => fireEvent.click(screen.getByTestId("save-channel")));
    expect(onSave.mock.calls[0][0].behavior.stabilizationSeconds).toBe(31);
    fireEvent.change(screen.getByLabelText("Wait for telemetry (minutes)"), { target: { value: "2.05" } });
    fireEvent.change(screen.getByLabelText("Max errors"), { target: { value: "2147483647" } });
    fireEvent.change(screen.getByLabelText("Max hashrate drop (%)"), { target: { value: "100" } });
    await act(async () => fireEvent.click(screen.getByTestId("save-channel")));
    expect(onSave.mock.calls[1][0].behavior).toMatchObject({
      stabilizationSeconds: 123,
      thresholds: { maxNewErrors: 2147483647, maxHashrateDropPercent: 100 },
    });
    fireEvent.change(screen.getByLabelText("Wait for telemetry (minutes)"), { target: { value: String(31 / 60) } });
    await act(async () => fireEvent.click(screen.getByTestId("save-channel")));
    expect(onSave.mock.calls[2][0].behavior.stabilizationSeconds).toBe(31);
    fireEvent.click(screen.getByLabelText("Review after each batch"));
    expect(screen.getByLabelText("Wait between batches (minutes)")).toHaveValue(String(1 / 60));
    await act(async () => fireEvent.click(screen.getByTestId("save-channel")));
    expect(onSave.mock.calls[3][0].behavior.waitBetweenBatchesSeconds).toBe(1);
    fireEvent.change(screen.getByLabelText("Wait between batches (minutes)"), { target: { value: "0.001" } });
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Wait between batches (minutes)"), { target: { value: "0.5" } });
    await act(async () => fireEvent.click(screen.getByTestId("save-channel")));
    expect(onSave.mock.calls[4][0].behavior.waitBetweenBatchesSeconds).toBe(30);
  });
});
