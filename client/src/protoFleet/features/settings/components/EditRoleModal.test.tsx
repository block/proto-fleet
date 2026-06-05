import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { pickRole, selectStubModule } from "./__testHelpers__/selectStub";
import EditRoleModal from "./EditRoleModal";
import { type RoleItem, useRoleManagement } from "@/protoFleet/api/useRoleManagement";
import { useUserManagement } from "@/protoFleet/api/useUserManagement";

vi.mock("@/protoFleet/api/useUserManagement");
vi.mock("@/protoFleet/api/useRoleManagement");
vi.mock("@/shared/features/toaster");
vi.mock("@/shared/components/Select", () => selectStubModule());

const mockUpdateUserRole = vi.fn();
const mockListRoles = vi.fn();
const mockOnDismiss = vi.fn();
const mockOnSuccess = vi.fn();

// Mirror the wire shape: server returns `role.name` as the seed identifier.
// formatRole() turns those into the labels the user sees.
const fakeRoles: RoleItem[] = [
  {
    roleId: "role-admin",
    name: "ADMIN",
    description: "Admin role",
    permissions: [],
    builtin: true,
    builtinKey: "ADMIN",
    memberCount: 0,
    updatedAt: null,
  },
  {
    roleId: "role-field-tech",
    name: "FIELD_TECH",
    description: "Field Tech role",
    permissions: [],
    builtin: true,
    builtinKey: "FIELD_TECH",
    memberCount: 0,
    updatedAt: null,
  },
];

const mockListRolesSuccess = (roles: RoleItem[] = fakeRoles) =>
  mockListRoles.mockImplementation(({ onSuccess, onFinally }) => {
    onSuccess?.(roles);
    onFinally?.();
  });

beforeEach(() => {
  vi.clearAllMocks();

  vi.mocked(useUserManagement).mockReturnValue({
    createUser: vi.fn(),
    listUsers: vi.fn(),
    resetUserPassword: vi.fn(),
    deactivateUser: vi.fn(),
    updateUserRole: mockUpdateUserRole,
  });

  vi.mocked(useRoleManagement).mockReturnValue({
    listRoles: mockListRoles,
    createRole: vi.fn(),
    updateRole: vi.fn(),
    deleteRole: vi.fn(),
  });

  mockListRolesSuccess();
});

describe("EditRoleModal", () => {
  const renderModal = (overrides: Partial<Parameters<typeof EditRoleModal>[0]> = {}) =>
    render(
      <EditRoleModal
        userId="user-1"
        username="alice"
        currentRoleName="FIELD_TECH"
        onDismiss={mockOnDismiss}
        onSuccess={mockOnSuccess}
        {...overrides}
      />,
    );

  it("renders the modal headed by the target username", () => {
    const { getByText } = renderModal();
    expect(getByText("Edit role for alice")).toBeInTheDocument();
  });

  it("preselects the user's current role", async () => {
    renderModal({ currentRoleName: "ADMIN" });

    await waitFor(() => {
      const select = screen.getByRole("combobox", { name: "Role" }) as HTMLSelectElement;
      expect(select.value).toBe("role-admin");
    });
  });

  it("keeps Save disabled when the picked role matches the current role", async () => {
    const { getByText } = renderModal({ currentRoleName: "ADMIN" });

    await waitFor(() => {
      const select = screen.getByRole("combobox", { name: "Role" }) as HTMLSelectElement;
      expect(select.value).toBe("role-admin");
    });

    const saveButton = getByText("Save").closest("button");
    expect(saveButton).toBeDisabled();
  });

  it("enables Save once a different role is picked", async () => {
    const { getByText } = renderModal({ currentRoleName: "ADMIN" });

    await waitFor(() => {
      const select = screen.getByRole("combobox", { name: "Role" }) as HTMLSelectElement;
      expect(select.value).toBe("role-admin");
    });

    pickRole("Field Tech");
    await waitFor(() => {
      const saveButton = getByText("Save").closest("button");
      expect(saveButton).not.toBeDisabled();
    });
  });

  it("calls updateUserRole with the chosen role and target user", async () => {
    mockUpdateUserRole.mockImplementation(({ onSuccess }) => onSuccess?.());

    const { getByText } = renderModal({ currentRoleName: "FIELD_TECH" });

    pickRole("Admin");
    fireEvent.click(getByText("Save"));

    await waitFor(() => {
      expect(mockUpdateUserRole).toHaveBeenCalledWith(
        expect.objectContaining({
          userId: "user-1",
          roleId: "role-admin",
        }),
      );
    });
    expect(mockOnSuccess).toHaveBeenCalled();
    expect(mockOnDismiss).toHaveBeenCalled();
  });

  it("surfaces server errors inline and keeps the modal open", async () => {
    mockUpdateUserRole.mockImplementation(({ onError }) => onError?.("Forbidden"));

    const { getByText, findByText } = renderModal({ currentRoleName: "FIELD_TECH" });

    pickRole("Admin");
    fireEvent.click(getByText("Save"));

    await findByText("Forbidden");
    expect(mockOnSuccess).not.toHaveBeenCalled();
    expect(mockOnDismiss).not.toHaveBeenCalled();
  });

  it("shows an inline error and disables Save when listRoles fails", async () => {
    mockListRoles.mockImplementation(({ onError, onFinally }) => {
      onError?.("Network error");
      onFinally?.();
    });

    const { getByText, findByText } = renderModal();

    await findByText(/Network error/);
    const saveButton = getByText("Save").closest("button");
    expect(saveButton).toBeDisabled();
  });

  it("filters out the SUPER_ADMIN role from the picker", async () => {
    mockListRoles.mockImplementation(({ onSuccess, onFinally }) => {
      onSuccess?.([
        ...fakeRoles,
        {
          roleId: "role-owner",
          name: "SUPER_ADMIN",
          description: "Owner role",
          permissions: [],
          builtin: true,
          builtinKey: "SUPER_ADMIN",
          memberCount: 0,
          updatedAt: null,
        },
      ]);
      onFinally?.();
    });

    renderModal();
    await waitFor(() => {
      const select = screen.getByRole("combobox", { name: "Role" });
      const labels = Array.from(select.querySelectorAll("option"))
        .map((o) => o.textContent ?? "")
        .filter((t) => t !== "Role");
      expect(labels).toEqual(["Admin", "Field Tech"]);
    });
  });
});
