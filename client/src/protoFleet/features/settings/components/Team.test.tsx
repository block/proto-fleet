import { type ReactNode } from "react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import Team from "./Team";

const permissionsMock = vi.hoisted(() => ({ current: [] as string[] }));
const listUsersMock = vi.hoisted(() => vi.fn());

vi.mock("@/protoFleet/store", () => ({
  useHasPermission: (permission: string) => permissionsMock.current.includes(permission),
  useRole: () => "SUPER_ADMIN",
  useUsername: () => "owner@example.com",
}));

vi.mock("@/protoFleet/api/useUserManagement", () => ({
  useUserManagement: () => ({
    listUsers: listUsersMock,
    resetUserPassword: vi.fn(),
    deactivateUser: vi.fn(),
  }),
}));

vi.mock("@/protoFleet/features/settings/components/Roles", () => ({
  default: () => <div data-testid="roles-panel">Roles panel</div>,
}));

vi.mock("@/protoFleet/features/settings/components/AddTeamMemberModal", () => ({
  default: () => null,
}));

vi.mock("@/protoFleet/features/settings/components/DeactivateUserDialog", () => ({
  default: () => null,
}));

vi.mock("@/protoFleet/features/settings/components/EditRoleModal", () => ({
  default: () => null,
}));

vi.mock("@/protoFleet/features/settings/components/ResetPasswordModal", () => ({
  default: () => null,
}));

vi.mock("@/shared/components/List", () => ({
  default: ({
    items,
    itemName,
    filters,
    headerControls,
    total,
  }: {
    items: unknown[];
    itemName: { singular: string; plural: string };
    filters?: { title: string }[];
    headerControls?: ReactNode;
    total?: number;
  }) => {
    const count = total ?? items.length;
    return (
      <div>
        <div data-testid="members-toolbar">
          {filters?.map((filter) => (
            <button key={filter.title} type="button" data-testid={`member-filter-${filter.title}`}>
              {filter.title}
            </button>
          ))}
          {headerControls}
        </div>
        <div data-testid="members-list">{`${count} ${count === 1 ? itemName.singular : itemName.plural}`}</div>
      </div>
    );
  },
}));

vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(),
  STATUSES: { error: "error", success: "success" },
}));

const LocationProbe = () => {
  const location = useLocation();
  return <div data-testid="location-probe">{`${location.pathname}${location.search}`}</div>;
};

const renderTeam = (initialEntry = "/settings/team") =>
  render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route path="/settings/team" element={<Team />} />
      </Routes>
      <LocationProbe />
    </MemoryRouter>,
  );

describe("Team", () => {
  beforeEach(() => {
    permissionsMock.current = ["user:read", "user:manage", "role:manage"];
    listUsersMock.mockReset();
    listUsersMock.mockImplementation(({ onSuccess, onFinally }) => {
      onSuccess([
        {
          userId: "1",
          username: "owner@example.com",
          passwordUpdatedAt: null,
          lastLoginAt: null,
          role: "SUPER_ADMIN",
          requiresPasswordChange: false,
        },
      ]);
      onFinally();
    });
  });

  it("defaults to members and can switch to roles", async () => {
    renderTeam();

    await waitFor(() => expect(screen.getByTestId("members-list").textContent).toBe("1 member"));
    expect(screen.queryByTestId("segmented-control")).not.toBeInTheDocument();
    expect(screen.getByTestId("team-tab-members")).toHaveAttribute("data-active", "true");
    expect(screen.getByTestId("member-filter-Role")).toBeInTheDocument();
    expect(screen.getByTestId("members-toolbar")).toContainElement(screen.getByText("Add team member"));

    fireEvent.click(screen.getByTestId("team-tab-roles-activate"));

    expect(screen.getByTestId("roles-panel")).toBeInTheDocument();
    expect(screen.getByTestId("team-tab-roles")).toHaveAttribute("data-active", "true");
    expect(screen.getByTestId("location-probe").textContent).toBe("/settings/team?tab=roles");
  });

  it("deep-links to the roles tab", () => {
    renderTeam("/settings/team?tab=roles");

    expect(screen.getByTestId("roles-panel")).toBeInTheDocument();
    expect(screen.queryByTestId("members-list")).not.toBeInTheDocument();
  });

  it("shows roles without loading members for role-only users", () => {
    permissionsMock.current = ["role:manage"];

    renderTeam();

    expect(screen.getByTestId("roles-panel")).toBeInTheDocument();
    expect(listUsersMock).not.toHaveBeenCalled();
  });
});
