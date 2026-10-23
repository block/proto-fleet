import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useTeamInvitations } from "./useTeamInvitations";

const { mockCreate, mockDelete, mockHandleAuthErrors, mockList, mockRevoke } = vi.hoisted(() => ({
  mockCreate: vi.fn(),
  mockDelete: vi.fn(),
  mockHandleAuthErrors: vi.fn(),
  mockList: vi.fn(),
  mockRevoke: vi.fn(),
}));

vi.mock("@/protoFleet/api/clients", () => ({
  invitationClient: {
    createTeamInvitation: mockCreate,
    listTeamInvitations: mockList,
    revokeTeamInvitation: mockRevoke,
    deleteTeamInvitation: mockDelete,
  },
}));

vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: () => ({ handleAuthErrors: mockHandleAuthErrors }),
}));

const timeoutOption = { timeoutMs: 30_000 };

describe("useTeamInvitations", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockCreate.mockResolvedValue({ token: "K7MP-4X9R" });
    mockList.mockResolvedValue({ invitations: [], nextPageToken: "" });
    mockRevoke.mockResolvedValue({});
    mockDelete.mockResolvedValue({});
  });

  it("adds bounded timeouts to invitation mutations", async () => {
    const { result } = renderHook(() => useTeamInvitations());

    await result.current.createTeamInvitation({ roleId: "role-1", expiresInSeconds: 3600 });
    await result.current.revokeTeamInvitation({ invitationId: "inv-1" });
    await result.current.deleteTeamInvitation({ invitationId: "inv-1" });

    expect(mockCreate).toHaveBeenCalledWith({ roleId: "role-1", expiresInSeconds: 3600 }, timeoutOption);
    expect(mockRevoke).toHaveBeenCalledWith({ invitationId: "inv-1" }, timeoutOption);
    expect(mockDelete).toHaveBeenCalledWith({ invitationId: "inv-1" }, timeoutOption);
  });

  it("keeps list cancellation and adds the bounded timeout", async () => {
    const controller = new AbortController();
    const { result } = renderHook(() => useTeamInvitations());

    await result.current.listTeamInvitations({ pageToken: "next", signal: controller.signal });

    expect(mockList).toHaveBeenCalledWith(
      { pageSize: 50, pageToken: "next" },
      { timeoutMs: 30_000, signal: controller.signal },
    );
  });
});
