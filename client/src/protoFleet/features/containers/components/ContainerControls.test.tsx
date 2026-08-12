import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import ContainerControls, { type ContainerToggleControl } from "./ContainerControls";

const controls: ContainerToggleControl[] = [
  { id: "ac-1", label: "AC 1", metric: "75°F", icon: "fan", on: true },
  { id: "pump", label: "Coolant pump", metric: "50 Hz", icon: "pump", on: false },
  { id: "auto", label: "Dry cooler auto", icon: "thermometer", on: true },
  { id: "light", label: "Tank A light", icon: "light", on: true },
];

const renderControls = () => {
  const onToggle = vi.fn();
  const onReset = vi.fn();
  const onMute = vi.fn();

  render(<ContainerControls controls={controls} onToggle={onToggle} alarm={{ label: "Alarm", onReset, onMute }} />);

  return { onToggle, onReset, onMute };
};

describe("ContainerControls", () => {
  it("renders the supplied inventory and only shows metrics that exist", () => {
    renderControls();

    expect(screen.getByText("AC 1")).toBeInTheDocument();
    expect(screen.getByText("75°F")).toBeInTheDocument();
    expect(screen.getByText("Coolant pump")).toBeInTheDocument();
    expect(screen.getByText("50 Hz")).toBeInTheDocument();
    expect(screen.getByText("Dry cooler auto").nextElementSibling).toBeNull();
    expect(screen.getAllByRole("checkbox")).toHaveLength(controls.length);
  });

  it("reports the control identity and next state without reading device state", async () => {
    const user = userEvent.setup();
    const { onToggle } = renderControls();

    await user.click(screen.getByRole("checkbox", { name: "AC 1 power" }));
    await user.click(screen.getByRole("checkbox", { name: "Coolant pump power" }));

    expect(onToggle).toHaveBeenNthCalledWith(1, "ac-1", false);
    expect(onToggle).toHaveBeenNthCalledWith(2, "pump", true);
  });

  it("keeps alarm commands separate from toggle controls", async () => {
    const user = userEvent.setup();
    const { onReset, onMute, onToggle } = renderControls();

    await user.click(screen.getByRole("button", { name: "Reset" }));
    await user.click(screen.getByRole("button", { name: "Mute" }));

    expect(onReset).toHaveBeenCalledTimes(1);
    expect(onMute).toHaveBeenCalledTimes(1);
    expect(onToggle).not.toHaveBeenCalled();
  });
});
