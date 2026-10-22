import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import TeamInvitations from "./TeamInvitations";
import { TeamInvitationSchema, TeamInvitationStatus } from "@/protoFleet/api/generated/invitation/v1/invitation_pb";
import { useTeamInvitations } from "@/protoFleet/api/useTeamInvitations";
import { PopoverProvider } from "@/shared/components/Popover";
import { pushToast } from "@/shared/features/toaster";

vi.mock("@/protoFleet/api/useTeamInvitations");
vi.mock("@/shared/features/toaster");

const mockList = vi.fn();
const mockRevoke = vi.fn();
const mockDelete = vi.fn();

const invitation = (invitationId: string, status: TeamInvitationStatus, inviterUsername = "owner") =>
  create(TeamInvitationSchema, {
    invitationId,
    roleId: "3",
    roleName: "FIELD_TECH",
    status,
    inviterUsername,
    createdAt: { seconds: 1_800_000_000n },
    expiresAt: { seconds: 1_800_086_400n },
  });

const rows = [
  invitation("pending", TeamInvitationStatus.PENDING),
  invitation("revoked", TeamInvitationStatus.REVOKED),
  invitation("expired", TeamInvitationStatus.EXPIRED),
];

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(useTeamInvitations).mockReturnValue({
    createTeamInvitation: vi.fn(),
    listTeamInvitations: mockList,
    revokeTeamInvitation: mockRevoke,
    deleteTeamInvitation: mockDelete,
  });
  mockList.mockImplementation(({ onSuccess, onFinally }) => {
    onSuccess?.(rows, "");
    onFinally?.();
  });
});

describe("TeamInvitations", () => {
  it("lists invitation history with readable statuses", async () => {
    render(<TeamInvitations refreshKey={0} />);

    expect(await screen.findByText("Pending")).toBeInTheDocument();
    expect(screen.getByText("Revoked")).toBeInTheDocument();
    expect(screen.getByText("Expired")).toBeInTheDocument();
    expect(screen.getAllByText("Field Tech")).toHaveLength(3);
  });

  it("refetches the first page when an invitation is created", async () => {
    const { rerender } = render(<TeamInvitations refreshKey={0} />);
    await screen.findByText("Pending");

    rerender(<TeamInvitations refreshKey={1} />);

    await waitFor(() => expect(mockList).toHaveBeenCalledTimes(2));
    expect(mockList).toHaveBeenLastCalledWith(expect.objectContaining({ pageToken: "" }));
  });

  it("revokes a pending invitation after confirmation", async () => {
    mockRevoke.mockImplementation(({ onSuccess, onFinally }) => {
      onSuccess?.();
      onFinally?.();
    });
    render(
      <PopoverProvider>
        <TeamInvitations refreshKey={0} />
      </PopoverProvider>,
    );
    await screen.findByText("Pending");

    // Only the pending row is revocable, so it is the only row with the action.
    const rowActions = screen.getAllByRole("button", { name: "Revoke" });
    expect(rowActions).toHaveLength(1);
    fireEvent.click(rowActions[0]);
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("Revoke invitation?")).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "Revoke" }));

    await waitFor(() => expect(mockRevoke).toHaveBeenCalledWith(expect.objectContaining({ invitationId: "pending" })));
  });

  it("deletes expired and revoked invitations without confirmation", async () => {
    mockDelete.mockImplementation(({ onSuccess }) => onSuccess?.());
    render(
      <PopoverProvider>
        <TeamInvitations refreshKey={0} />
      </PopoverProvider>,
    );
    await screen.findByText("Pending");

    const deleteActions = screen.getAllByRole("button", { name: "Delete" });
    expect(deleteActions).toHaveLength(2);
    fireEvent.click(deleteActions[0]);

    await waitFor(() => expect(mockDelete).toHaveBeenCalledWith(expect.objectContaining({ invitationId: "revoked" })));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    await waitFor(() => expect(mockList).toHaveBeenCalledTimes(2));
  });

  it("disables delete while a delete request is pending", async () => {
    render(
      <PopoverProvider>
        <TeamInvitations refreshKey={0} />
      </PopoverProvider>,
    );
    await screen.findByText("Pending");

    const deleteAction = screen.getAllByRole("button", { name: "Delete" })[0];
    fireEvent.click(deleteAction);

    expect(mockDelete).toHaveBeenCalledTimes(1);
    expect(deleteAction).toBeDisabled();
    fireEvent.click(deleteAction);
    expect(mockDelete).toHaveBeenCalledTimes(1);
  });

  it("offers revocation, not deletion, for an unavailable invitation", async () => {
    mockList.mockImplementation(({ onSuccess, onFinally }) => {
      onSuccess?.([invitation("unavailable", TeamInvitationStatus.UNAVAILABLE)], "");
      onFinally?.();
    });
    render(
      <PopoverProvider>
        <TeamInvitations refreshKey={0} />
      </PopoverProvider>,
    );

    expect(await screen.findByText("Unavailable")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Revoke" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Delete" })).not.toBeInTheDocument();
  });

  describe("pagination", () => {
    type ListRequest = {
      pageToken: string;
      signal?: AbortSignal;
      onSuccess?: (rows: ReturnType<typeof invitation>[], next: string) => void;
      onError?: (message: string) => void;
      onFinally?: () => void;
    };
    let requests: ListRequest[];

    beforeEach(() => {
      requests = [];
      mockList.mockImplementation((request: ListRequest) => requests.push(request));
    });

    // Each page's inviter name identifies which response is on screen.
    const respond = (index: number, inviter: string, next = "") => {
      act(() => {
        requests[index].onSuccess?.([invitation(inviter, TeamInvitationStatus.PENDING, inviter)], next);
        requests[index].onFinally?.();
      });
    };

    it("ignores repeated Next clicks while the page loads", async () => {
      render(<TeamInvitations refreshKey={0} />);
      respond(0, "first-page", "page-2");
      const next = await screen.findByRole("button", { name: "Next" });

      fireEvent.click(next);
      fireEvent.click(next);

      await waitFor(() => expect(requests).toHaveLength(2));
      expect(requests[1].pageToken).toBe("page-2");
      expect(next).toBeDisabled();
      respond(1, "second-page");
      expect(await screen.findByText("second-page")).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "Previous" })).toBeEnabled();
      expect(requests).toHaveLength(2);
    });

    it("keeps the latest page when an earlier response arrives late", async () => {
      const { rerender } = render(<TeamInvitations refreshKey={0} />);
      respond(0, "first-page", "page-2");
      fireEvent.click(await screen.findByRole("button", { name: "Next" }));
      await waitFor(() => expect(requests).toHaveLength(2));

      rerender(<TeamInvitations refreshKey={1} />);
      await waitFor(() => expect(requests).toHaveLength(3));
      expect(requests[2].pageToken).toBe("");
      respond(2, "refreshed-first-page", "page-2");
      respond(1, "stale-second-page");

      expect(screen.getByText("refreshed-first-page")).toBeInTheDocument();
      expect(screen.queryByText("stale-second-page")).not.toBeInTheDocument();
      expect(screen.getByRole("button", { name: "Next" })).toBeEnabled();
      expect(screen.getByRole("button", { name: "Previous" })).toBeDisabled();
      expect(requests).toHaveLength(3);
    });

    it("ignores stale errors after unmounting the list", async () => {
      const { unmount } = render(<TeamInvitations refreshKey={0} />);
      await waitFor(() => expect(requests).toHaveLength(1));

      unmount();
      act(() => {
        requests[0].onError?.("late failure");
        requests[0].onFinally?.();
      });

      expect(requests[0].signal?.aborted).toBe(true);
      expect(pushToast).not.toHaveBeenCalled();
    });
  });
});
