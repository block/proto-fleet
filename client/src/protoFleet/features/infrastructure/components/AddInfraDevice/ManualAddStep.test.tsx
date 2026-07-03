import type { ComponentProps } from "react";
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

const renderManualAddStep = (props: Partial<ComponentProps<typeof ManualAddStep>> = {}) => {
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
      {...props}
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
      unitId: 17,
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

  test("requires catalog-backed location selections", async () => {
    const user = userEvent.setup();
    const onSuccess = vi.fn();
    let currentState: ManualAddStepState | undefined;

    render(
      <ManualAddStep
        onSuccess={onSuccess}
        onStateChange={(state) => {
          currentState = state;
        }}
      />,
    );

    expect(screen.getByLabelText("Site")).toBeDisabled();
    expect(screen.getByLabelText("Building")).toBeDisabled();

    await user.type(screen.getByLabelText("Name"), "Roof exhaust");
    await user.type(screen.getByLabelText("Unit ID"), "17");
    await user.type(screen.getByLabelText("Endpoint"), "10.12.1.21");
    await user.type(screen.getByLabelText("Port"), "502");

    await waitFor(() => expect(currentState?.canAdd).toBe(false));
    currentState?.addHandler();
    expect(onSuccess).not.toHaveBeenCalled();
  });

  test("requires Unit ID to be within the Modbus unit address range", async () => {
    const user = userEvent.setup();
    const { getState } = renderManualAddStep();

    await user.type(screen.getByLabelText("Name"), "Roof exhaust");
    await user.selectOptions(screen.getByLabelText("Site"), "Austin");
    await user.selectOptions(screen.getByLabelText("Building"), "Building 1");
    await user.type(screen.getByLabelText("Endpoint"), "10.12.1.21");
    await user.type(screen.getByLabelText("Port"), "502");

    await user.type(screen.getByLabelText("Unit ID"), "248");
    expect(getState()?.canAdd).toBe(false);

    await user.clear(screen.getByLabelText("Unit ID"));
    await user.type(screen.getByLabelText("Unit ID"), "247");

    await waitFor(() => expect(getState()?.canAdd).toBe(true));
  });

  test("preselects the initial site", () => {
    renderManualAddStep({ initialSiteName: "Denver" });

    expect(screen.getByLabelText("Site")).toHaveValue("Denver");
  });
});
