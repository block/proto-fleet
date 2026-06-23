import { MemoryRouter } from "react-router-dom";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import EnergyPage from "@/protoFleet/features/energy/pages/EnergyPage";
import { useHasPermission } from "@/protoFleet/store";

vi.mock("@/protoFleet/store", () => ({
  useHasPermission: vi.fn(),
}));

vi.mock("@/protoFleet/features/energy/CurtailmentManagementPanel", () => ({
  default: ({ canManageCurtailment }: { canManageCurtailment?: boolean }) => (
    <div data-testid="curtailment-management-panel">{String(canManageCurtailment)}</div>
  ),
}));

describe("EnergyPage", () => {
  beforeEach(() => {
    vi.mocked(useHasPermission).mockReset();
  });

  it("passes curtailment manage permission to the management panel", () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:read");

    render(
      <MemoryRouter>
        <EnergyPage />
      </MemoryRouter>,
    );

    expect(useHasPermission).toHaveBeenCalledWith("curtailment:read");
    expect(useHasPermission).toHaveBeenCalledWith("curtailment:manage");
    expect(screen.getByTestId("curtailment-management-panel")).toHaveTextContent("false");
  });

  it("redirects without curtailment read permission", () => {
    vi.mocked(useHasPermission).mockReturnValue(false);

    render(
      <MemoryRouter>
        <EnergyPage />
      </MemoryRouter>,
    );

    expect(useHasPermission).toHaveBeenCalledWith("curtailment:read");
    expect(screen.queryByTestId("curtailment-management-panel")).not.toBeInTheDocument();
  });
});
