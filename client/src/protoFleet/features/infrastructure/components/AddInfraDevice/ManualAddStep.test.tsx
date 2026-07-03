import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import ManualAddStep, { type ManualAddStepState } from "./ManualAddStep";

vi.mock("@/shared/components/Select", () => ({
  default: ({
    id,
    label,
    options,
    value,
    onChange,
    disabled,
  }: {
    id: string;
    label: string;
    options: { value: string; label: string }[];
    value: string;
    onChange: (value: string) => void;
    disabled?: boolean;
  }) => (
    <label htmlFor={id}>
      {label}
      <select id={id} value={value} disabled={disabled} onChange={(event) => onChange(event.currentTarget.value)}>
        <option value="" />
        {options.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
    </label>
  ),
}));

const renderManualAddStep = () => {
  const onSuccess = vi.fn();
  let currentState: ManualAddStepState | undefined;

  render(
    <ManualAddStep
      siteOptions={["Austin", "Denver"]}
      buildingOptions={[
        { siteName: "Austin", buildingName: "Building 1" },
        { siteName: "Austin", buildingName: "Building 10" },
        { siteName: "Denver", buildingName: "Denver Plant" },
      ]}
      onSuccess={onSuccess}
      onStateChange={(state) => {
        currentState = state;
      }}
    />,
  );

  return {
    onSuccess,
    getState: () => currentState,
  };
};

describe("ManualAddStep", () => {
  test("submits unit ID, selected target type, and fan count with Modbus TCP", async () => {
    const user = userEvent.setup();
    const { getState, onSuccess } = renderManualAddStep();

    await user.type(screen.getByLabelText("Name"), "Roof exhaust");
    expect(screen.getByRole("button", { name: "About Unit ID" })).toBeInTheDocument();
    await user.type(screen.getByLabelText("Unit ID"), "17");
    await user.selectOptions(screen.getByLabelText("Site"), "Austin");
    await user.selectOptions(screen.getByLabelText("Building"), "Building 1");
    await user.selectOptions(screen.getByLabelText("Target type"), "fan_group");
    await user.clear(screen.getByLabelText("Fans"));
    await user.type(screen.getByLabelText("Fans"), "12");
    expect(screen.getByLabelText("Connection type")).toHaveValue("Modbus TCP");
    expect(screen.getByLabelText("Connection type")).toHaveAttribute("readonly");
    await user.type(screen.getByLabelText("Endpoint"), "10.12.1.21");
    await user.type(screen.getByLabelText("Port"), "502");

    await waitFor(() => expect(getState()?.canAdd).toBe(true));
    getState()?.addHandler();

    expect(onSuccess).toHaveBeenCalledWith({
      id: "17",
      name: "Roof exhaust",
      siteName: "Austin",
      buildingName: "Building 1",
      endpointKind: "fan_group",
      fanCount: 12,
      connectionType: "modbus_tcp",
      endpoint: "10.12.1.21",
      port: 502,
    });
  });
});
