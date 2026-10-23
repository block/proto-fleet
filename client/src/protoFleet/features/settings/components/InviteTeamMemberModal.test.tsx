import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { pickRole, selectStubModule } from "./__testHelpers__/selectStub";
import InviteTeamMemberModal from "./InviteTeamMemberModal";
import { type RoleItem, useRoleManagement } from "@/protoFleet/api/useRoleManagement";
import { useTeamInvitations } from "@/protoFleet/api/useTeamInvitations";

vi.mock("@/protoFleet/api/useTeamInvitations");
vi.mock("@/protoFleet/api/useRoleManagement");
vi.mock("@/shared/features/toaster");
vi.mock("@/shared/components/Select", () => selectStubModule());

const mockCreateTeamInvitation = vi.fn();
const mockListRoles = vi.fn();
const mockOnDismiss = vi.fn();
const mockOnCreated = vi.fn();

const role = (roleId: string, name: string): RoleItem => ({
  roleId,
  name,
  description: `${name} role`,
  permissions: [],
  builtin: true,
  builtinKey: name as RoleItem["builtinKey"],
  memberCount: 0,
  updatedAt: null,
});
const fakeRoles = [role("1", "SUPER_ADMIN"), role("2", "ADMIN"), role("3", "FIELD_TECH")];

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(useTeamInvitations).mockReturnValue({
    createTeamInvitation: mockCreateTeamInvitation,
    listTeamInvitations: vi.fn(),
    revokeTeamInvitation: vi.fn(),
    deleteTeamInvitation: vi.fn(),
  });
  vi.mocked(useRoleManagement).mockReturnValue({
    listRoles: mockListRoles,
    createRole: vi.fn(),
    updateRole: vi.fn(),
    deleteRole: vi.fn(),
  });
  mockListRoles.mockImplementation(({ onSuccess, onFinally }) => {
    onSuccess?.(fakeRoles);
    onFinally?.();
  });
});

const renderModal = () => render(<InviteTeamMemberModal onDismiss={mockOnDismiss} onCreated={mockOnCreated} />);
const createButton = () => screen.getByText("Create invitation").closest("button");

describe("InviteTeamMemberModal", () => {
  it("offers assignable roles only and keeps Create disabled until one is picked", async () => {
    renderModal();

    const options = Array.from(screen.getByRole("combobox", { name: "Role" }).querySelectorAll("option")).map(
      (option) => option.textContent,
    );
    expect(options).toEqual(["Role", "Admin", "Field Tech"]);
    await waitFor(() => expect(createButton()).toBeDisabled());
    pickRole("Field Tech");
    expect(createButton()).not.toBeDisabled();
  });

  it("stays disabled and explains why when roles fail to load", async () => {
    mockListRoles.mockImplementation(({ onError, onFinally }) => {
      onError?.("Network error");
      onFinally?.();
    });
    renderModal();

    expect(await screen.findByText(/Network error/)).toBeInTheDocument();
    expect(createButton()).toBeDisabled();
  });

  it("creates an invitation with the chosen role and expiry, then shows the code once", async () => {
    mockCreateTeamInvitation.mockImplementation(({ onSuccess, onFinally }) => {
      onSuccess?.("K7MP-4X9R");
      onFinally?.();
    });
    renderModal();

    pickRole("Admin");
    fireEvent.change(screen.getByRole("combobox", { name: "Expires after" }), { target: { value: "3600" } });
    fireEvent.click(screen.getByText("Create invitation"));

    await waitFor(() =>
      expect(mockCreateTeamInvitation).toHaveBeenCalledWith(
        expect.objectContaining({ roleId: "2", expiresInSeconds: 3600 }),
      ),
    );
    expect(mockOnCreated).toHaveBeenCalled();
    expect(await screen.findByText("Share invitation")).toBeInTheDocument();
    expect(screen.getByLabelText("Invitation code")).toHaveValue("K7MP-4X9R");
    fireEvent.click(screen.getByText("Done"));
    expect(mockOnDismiss).toHaveBeenCalled();
  });

  it("shows the server's reason when the role cannot be granted", async () => {
    mockCreateTeamInvitation.mockImplementation(({ onError, onFinally }) => {
      onError?.("insufficient permissions to manage this user");
      onFinally?.();
    });
    renderModal();

    pickRole("Admin");
    fireEvent.click(screen.getByText("Create invitation"));

    expect(await screen.findByText("insufficient permissions to manage this user")).toBeInTheDocument();
    expect(mockOnCreated).not.toHaveBeenCalled();
  });

  it("cannot be dismissed while the invitation is being created", async () => {
    let finish: (() => void) | undefined;
    mockCreateTeamInvitation.mockImplementation(({ onError, onFinally }) => {
      finish = () => {
        onError?.("network error");
        onFinally?.();
      };
    });
    renderModal();

    pickRole("Admin");
    fireEvent.click(screen.getByText("Create invitation"));
    await waitFor(() => expect(mockCreateTeamInvitation).toHaveBeenCalled());
    fireEvent.keyDown(document, { key: "Escape" });
    expect(mockOnDismiss).not.toHaveBeenCalled();

    act(() => finish?.());
    expect(await screen.findByText("network error")).toBeInTheDocument();
    fireEvent.keyDown(document, { key: "Escape" });
    expect(mockOnDismiss).toHaveBeenCalled();
  });
});
