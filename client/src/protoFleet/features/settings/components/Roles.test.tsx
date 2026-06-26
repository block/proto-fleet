import { MemoryRouter, Route, Routes } from "react-router-dom";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import Roles from "./Roles";

const permissionsMock = vi.hoisted(() => ({ current: [] as string[] }));
const listRolesMock = vi.hoisted(() => vi.fn());
const deleteRoleMock = vi.hoisted(() => vi.fn());

vi.mock("@/protoFleet/store", () => ({
  useHasPermission: (permission: string) => permissionsMock.current.includes(permission),
}));

vi.mock("@/protoFleet/api/useRoleManagement", () => ({
  isImmutable: (role: { builtin: boolean }) => role.builtin === true,
  useRoleManagement: () => ({
    listRoles: listRolesMock,
    deleteRole: deleteRoleMock,
  }),
}));

vi.mock("@/protoFleet/features/settings/components/CreateEditRoleModal", () => ({
  default: () => null,
}));

vi.mock("@/protoFleet/features/settings/components/DeleteRoleDialog", () => ({
  default: () => null,
}));

vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(),
  STATUSES: { error: "error", success: "success" },
}));

const renderRoles = () =>
  render(
    <MemoryRouter initialEntries={["/settings/team?tab=roles"]}>
      <Routes>
        <Route path="/settings/team" element={<Roles embedded />} />
      </Routes>
    </MemoryRouter>,
  );

describe("Roles", () => {
  beforeEach(() => {
    permissionsMock.current = ["role:manage"];
    deleteRoleMock.mockReset();
    listRolesMock.mockReset();
    listRolesMock.mockImplementation(({ onSuccess, onFinally }) => {
      onSuccess([
        {
          roleId: "admin",
          name: "Admin",
          description: "Full access",
          permissions: ["fleet:read", "role:manage"],
          builtin: true,
          builtinKey: "ADMIN",
          memberCount: 4,
          updatedAt: new Date("2026-06-01T12:00:00Z"),
        },
      ]);
      onFinally();
    });
  });

  it("shows a popover explaining system default roles", async () => {
    renderRoles();

    await screen.findByTestId("system-role-lock");
    const lockButton = screen.getByRole("button", { name: "System default role" });
    expect(screen.queryByTestId("system-role-lock-popover")).not.toBeInTheDocument();

    fireEvent.click(lockButton);

    await waitFor(() => expect(screen.getByTestId("system-role-lock-popover")).toBeInTheDocument());
    expect(
      screen.getByText("This is a system default role. Built-in roles cannot be edited or deleted."),
    ).toBeInTheDocument();
    expect(screen.getByTestId("system-role-lock-popover").querySelector(".popover-content")).toHaveClass("w-80");

    fireEvent.mouseDown(document.body);

    await waitFor(() => expect(screen.queryByTestId("system-role-lock-popover")).not.toBeInTheDocument());
  });
});
