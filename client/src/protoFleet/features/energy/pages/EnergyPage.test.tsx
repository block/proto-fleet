import { MemoryRouter } from "react-router-dom";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import EnergyPage from "@/protoFleet/features/energy/pages/EnergyPage";
import { useHasPermission, useRole } from "@/protoFleet/store";

vi.mock("@/protoFleet/store", () => ({
  useHasPermission: vi.fn(),
  useRole: vi.fn(),
}));

vi.mock("@/protoFleet/features/energy/CurtailmentManagementPanel", () => ({
  default: ({ enableManage, enableRecover }: { enableManage?: boolean; enableRecover?: boolean }) => (
    <div data-testid="curtailment-management-panel">
      {String(enableManage)},{String(enableRecover)}
    </div>
  ),
}));

describe("EnergyPage", () => {
  beforeEach(() => {
    vi.mocked(useHasPermission).mockReset();
    vi.mocked(useRole).mockReturnValue("FIELD_TECH");
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
    expect(screen.getByTestId("curtailment-management-panel")).toHaveTextContent("false,false");
  });

  it("passes admin recovery access for admin managers", () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:read" || key === "curtailment:manage");
    vi.mocked(useRole).mockReturnValue("ADMIN");

    render(
      <MemoryRouter>
        <EnergyPage />
      </MemoryRouter>,
    );

    expect(screen.getByTestId("curtailment-management-panel")).toHaveTextContent("true,true");
  });

  it("withholds admin recovery access when admin lacks curtailment manage permission", () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:read");
    vi.mocked(useRole).mockReturnValue("ADMIN");

    render(
      <MemoryRouter>
        <EnergyPage />
      </MemoryRouter>,
    );

    expect(screen.getByTestId("curtailment-management-panel")).toHaveTextContent("false,false");
  });

  it("passes admin recovery access for super admin managers", () => {
    vi.mocked(useHasPermission).mockImplementation((key) => key === "curtailment:read" || key === "curtailment:manage");
    vi.mocked(useRole).mockReturnValue("SUPER_ADMIN");

    render(
      <MemoryRouter>
        <EnergyPage />
      </MemoryRouter>,
    );

    expect(screen.getByTestId("curtailment-management-panel")).toHaveTextContent("true,true");
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
