import { MemoryRouter } from "react-router-dom";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import ContainerOverview, { type ContainerOverviewProps } from "./ContainerOverview";

vi.mock("@/protoFleet/features/groupManagement/components/DeviceSetPerformanceSection", () => ({
  DeviceSetPerformanceSection: () => <div data-testid="performance-section" />,
}));

const controls: NonNullable<ContainerOverviewProps["controls"]> = [
  { id: "coolant-pump", label: "Coolant pump", icon: "pump", on: true },
];

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
});
