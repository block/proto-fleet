import { MemoryRouter } from "react-router-dom";
import { render, screen, waitForElementToBeRemoved } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import ContainerOverview, { type ContainerOverviewProps } from "./ContainerOverview";

vi.mock("@/protoFleet/features/groupManagement/components/DeviceSetPerformanceSection", () => ({
  DeviceSetPerformanceSection: () => <div data-testid="performance-section" />,
}));

const controls: NonNullable<ContainerOverviewProps["controls"]> = [
  { id: "coolant-pump", label: "Coolant pump", icon: "pump", on: true },
];

const TEST_TANK = {
  id: "tank-1",
  label: "Tank 1",
  on: true,
  cols: 1,
  rows: 1,
  modules: ["healthy" as const],
  stats: ["1/1 module", "65.5°", "12.3 kW"],
  tempLabel: "65.5°",
  powerLabel: "12.3 kW",
};

const TEST_FAN = {
  id: "fan-1",
  label: "Fan 1",
  on: true,
  speedPercent: 68,
  speedLabel: "3,200",
};

const renderOverview = (overrides: Partial<ContainerOverviewProps> = {}) => {
  const props: ContainerOverviewProps = {
    breadcrumb: [{ label: "Container" }],
    title: "CT1-01",
    kpis: [],
    tanks: [],
    fans: [],
    controls,
    onToggleTank: vi.fn(),
    onToggleFan: vi.fn(),
    onToggleControl: vi.fn(),
    onResetAlarm: vi.fn(),
    onMuteAlarm: vi.fn(),
    ...overrides,
  };

  render(
    <MemoryRouter>
      <ContainerOverview {...props} />
    </MemoryRouter>,
  );
};

describe("ContainerOverview", () => {
  it.each(["onResetAlarm", "onMuteAlarm"] as const)("hides Controls when %s is not provided", (missingCallback) => {
    renderOverview({ [missingCallback]: undefined });

    expect(screen.queryByTestId("container-controls")).not.toBeInTheDocument();
  });

  it("renders Controls when the complete callback set is provided", () => {
    renderOverview();

    expect(screen.getByTestId("container-controls")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Reset" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Mute" })).toBeEnabled();
  });

  it("confirms a tank PDU toggle before calling the parent handler", async () => {
    const user = userEvent.setup();
    const onToggleTank = vi.fn();
    renderOverview({ tanks: [TEST_TANK], onToggleTank });

    await user.click(screen.getByRole("checkbox", { name: "Tank 1 power" }));

    expect(onToggleTank).not.toHaveBeenCalled();
    expect(await screen.findByText("Power off Tank 1?")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Power off" }));
    expect(onToggleTank).toHaveBeenCalledOnce();
    expect(onToggleTank).toHaveBeenCalledWith("tank-1", false);
  });

  it("cancels a pending tank PDU toggle without calling the parent handler", async () => {
    const user = userEvent.setup();
    const onToggleTank = vi.fn();
    renderOverview({ tanks: [TEST_TANK], onToggleTank });

    await user.click(screen.getByRole("checkbox", { name: "Tank 1 power" }));
    const dialog = await screen.findByTestId("tank-power-confirm");
    await user.click(screen.getByRole("button", { name: "Cancel" }));

    expect(onToggleTank).not.toHaveBeenCalled();
    await waitForElementToBeRemoved(dialog);
  });

  it("opens the built-in tank status glance when no override is supplied", async () => {
    const user = userEvent.setup();
    renderOverview({ tanks: [TEST_TANK] });

    await user.click(screen.getByRole("button", { name: "Tank 1 info" }));

    expect(await screen.findByText("Tank status")).toBeInTheDocument();
    expect(screen.getByText("Tank 1 is operating normally")).toBeInTheDocument();
  });

  it("uses the tank info override instead of opening the built-in glance", async () => {
    const user = userEvent.setup();
    const onTankInfo = vi.fn();
    renderOverview({ tanks: [TEST_TANK], onTankInfo });

    await user.click(screen.getByRole("button", { name: "Tank 1 info" }));

    expect(onTankInfo).toHaveBeenCalledWith("tank-1");
    expect(screen.queryByText("Tank status")).not.toBeInTheDocument();
  });

  it("opens the built-in fan status glance when no override is supplied", async () => {
    const user = userEvent.setup();
    renderOverview({ fans: [TEST_FAN] });

    await user.click(screen.getByRole("button", { name: "Fan 1 info" }));

    expect(await screen.findByText("Fan status")).toBeInTheDocument();
    expect(screen.getByText("Fan 1 is operating normally")).toBeInTheDocument();
  });
});
