import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { type TeamInvitation, TeamInvitationStatus } from "@/protoFleet/api/generated/invitation/v1/invitation_pb";
import { useTeamInvitations } from "@/protoFleet/api/useTeamInvitations";
import { PageControls } from "@/protoFleet/features/hostedAccounts/components/PageControls";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import { formatRole } from "@/protoFleet/features/settings/utils/formatRole";
import { Alert, Trash } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";
import List from "@/shared/components/List";
import { ColConfig, ColTitles } from "@/shared/components/List/types";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { formatTimestamp } from "@/shared/utils/formatTimestamp";

type InvitationColumns = "role" | "status" | "inviter" | "createdAt" | "expiresAt";

const colTitles: ColTitles<InvitationColumns> = {
  role: "Role",
  status: "Status",
  inviter: "Invited by",
  createdAt: "Created",
  expiresAt: "Expires",
};
const activeCols: InvitationColumns[] = ["role", "status", "inviter", "createdAt", "expiresAt"];

const invitationStatus = (invitation: TeamInvitation) => {
  switch (invitation.status) {
    case TeamInvitationStatus.PENDING:
      return "Pending";
    case TeamInvitationStatus.REVOKED:
      return "Revoked";
    case TeamInvitationStatus.EXPIRED:
      return "Expired";
    default:
      return "Unavailable";
  }
};

const revocable = (invitation: TeamInvitation) =>
  invitation.status === TeamInvitationStatus.PENDING || invitation.status === TeamInvitationStatus.UNAVAILABLE;
const deletable = (invitation: TeamInvitation) =>
  invitation.status === TeamInvitationStatus.REVOKED || invitation.status === TeamInvitationStatus.EXPIRED;

const timestampCell = (timestamp?: { seconds: bigint }) => (
  <span>{timestamp ? formatTimestamp(Number(timestamp.seconds)) : "—"}</span>
);

interface TeamInvitationsProps {
  /** Changes whenever an invitation is created elsewhere on the page. */
  refreshKey: number;
  headerControls?: ReactNode;
  filtersClassName?: string;
}

/** A page index and the cursors that reach every page up to it. */
type PageView = { page: number; pageTokens: string[] };
const firstPage: PageView = { page: 0, pageTokens: [""] };

const TeamInvitations = ({ refreshKey, headerControls, filtersClassName }: TeamInvitationsProps) => {
  const { listTeamInvitations, revokeTeamInvitation, deleteTeamInvitation } = useTeamInvitations();
  const [invitations, setInvitations] = useState<TeamInvitation[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [view, setView] = useState<PageView>(firstPage);
  const [nextPageToken, setNextPageToken] = useState("");
  const [revoking, setRevoking] = useState<TeamInvitation | null>(null);
  const [isRevoking, setIsRevoking] = useState(false);
  const pendingDeletes = useRef(new Set<string>());
  const [deletingInvitationIds, setDeletingInvitationIds] = useState<Set<string>>(() => new Set());
  const mounted = useRef(true);

  // The page on screen changes only when a requested page loads, so a failed
  // request keeps the current rows, page index and cursors together. Starting
  // a load abandons the previous one.
  const shownView = useRef<PageView>(firstPage);
  const inFlight = useRef<AbortController | null>(null);
  const loadPage = useCallback(
    (target: PageView) => {
      if (!mounted.current) return;
      inFlight.current?.abort();
      const controller = new AbortController();
      inFlight.current = controller;
      setIsLoading(true);
      listTeamInvitations({
        pageToken: target.pageTokens[target.page],
        signal: controller.signal,
        onSuccess: (rows, next) => {
          if (controller.signal.aborted) return;
          shownView.current = target;
          setView(target);
          setInvitations(rows);
          setNextPageToken(next);
        },
        onError: (error) => {
          if (controller.signal.aborted) return;
          pushToast({ message: error || "Failed to load invitations", status: STATUSES.error });
        },
        onFinally: () => {
          if (controller.signal.aborted) return;
          inFlight.current = null;
          setIsLoading(false);
        },
      });
    },
    [listTeamInvitations],
  );
  // Revocation and deletion finish asynchronously; refresh whichever page is
  // on screen by then.
  const reloadShownPage = useCallback(() => {
    if (!mounted.current) return;
    loadPage(shownView.current);
  }, [loadPage]);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      inFlight.current?.abort();
    };
  }, []);
  // A new invitation is always newest, so creation returns to the first page.
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- fetch on mount and after creation; setState inside async fetch is the external-sync pattern
    loadPage(firstPage);
  }, [loadPage, refreshKey]);

  const handleRevokeConfirm = useCallback(() => {
    if (!revoking) return;
    const invitationId = revoking.invitationId;
    setIsRevoking(true);
    revokeTeamInvitation({
      invitationId,
      onSuccess: () => {
        if (!mounted.current) return;
        pushToast({ message: "Invitation revoked", status: STATUSES.success });
        setRevoking((current) => (current?.invitationId === invitationId ? null : current));
        reloadShownPage();
      },
      onError: (error) => {
        if (!mounted.current) return;
        pushToast({ message: error || "Failed to revoke invitation", status: STATUSES.error });
      },
      onFinally: () => {
        if (!mounted.current) return;
        setIsRevoking(false);
      },
    });
  }, [reloadShownPage, revokeTeamInvitation, revoking]);

  // Dead invitations grant nothing, so removing one needs no confirmation.
  const handleDelete = useCallback(
    (invitation: TeamInvitation) => {
      if (pendingDeletes.current.has(invitation.invitationId)) return;
      pendingDeletes.current.add(invitation.invitationId);
      setDeletingInvitationIds(new Set(pendingDeletes.current));
      deleteTeamInvitation({
        invitationId: invitation.invitationId,
        onSuccess: () => {
          if (!mounted.current) return;
          pushToast({ message: "Invitation deleted", status: STATUSES.success });
          reloadShownPage();
        },
        onError: (error) => {
          if (!mounted.current) return;
          pushToast({ message: error || "Failed to delete invitation", status: STATUSES.error });
        },
        onFinally: () => {
          if (!mounted.current) return;
          pendingDeletes.current.delete(invitation.invitationId);
          setDeletingInvitationIds(new Set(pendingDeletes.current));
        },
      });
    },
    [deleteTeamInvitation, reloadShownPage],
  );

  const colConfig: ColConfig<TeamInvitation, string, InvitationColumns> = useMemo(
    () => ({
      role: { component: (invitation) => <span>{formatRole(invitation.roleName)}</span>, width: "w-40" },
      status: { component: (invitation) => <span>{invitationStatus(invitation)}</span>, width: "w-56" },
      inviter: { component: (invitation) => <span>{invitation.inviterUsername}</span>, width: "w-48" },
      createdAt: { component: (invitation) => timestampCell(invitation.createdAt), width: "w-48" },
      expiresAt: {
        component: (invitation) =>
          timestampCell(invitation.status === TeamInvitationStatus.REVOKED ? undefined : invitation.expiresAt),
        width: "w-48",
      },
    }),
    [],
  );

  const actions = useMemo(
    () => [
      {
        title: "Revoke",
        icon: <Trash />,
        variant: "destructive" as const,
        actionHandler: (invitation: TeamInvitation) => setRevoking(invitation),
        hidden: (invitation: TeamInvitation) => !revocable(invitation),
      },
      {
        title: "Delete",
        icon: <Trash />,
        variant: "destructive" as const,
        actionHandler: handleDelete,
        disabled: (invitation: TeamInvitation) => deletingInvitationIds.has(invitation.invitationId),
        hidden: (invitation: TeamInvitation) => !deletable(invitation),
      },
    ],
    [deletingInvitationIds, handleDelete],
  );

  return (
    <>
      {isLoading && invitations.length === 0 ? (
        <div className="text-center text-text-primary-50">Loading invitations...</div>
      ) : (
        <List<TeamInvitation, string, InvitationColumns>
          items={invitations}
          itemKey="invitationId"
          activeCols={activeCols}
          colTitles={colTitles}
          colConfig={colConfig}
          filtersClassName={filtersClassName}
          headerControls={headerControls}
          itemName={{ singular: "invitation", plural: "invitations" }}
          stickyFirstColumn={false}
          noDataElement={
            <SettingsEmptyState
              title="No invitations yet"
              description="Invite a team member to get a single-use code to share."
            />
          }
          actions={actions}
        />
      )}
      <PageControls
        previous={view.page > 0}
        next={Boolean(nextPageToken)}
        disabled={isLoading}
        onPrevious={() => loadPage({ page: view.page - 1, pageTokens: view.pageTokens })}
        onNext={() =>
          loadPage({ page: view.page + 1, pageTokens: [...view.pageTokens.slice(0, view.page + 1), nextPageToken] })
        }
      />
      <Dialog
        open={Boolean(revoking)}
        title="Revoke invitation?"
        onDismiss={isRevoking ? undefined : () => setRevoking(null)}
        icon={
          <DialogIcon intent="critical">
            <Alert />
          </DialogIcon>
        }
        buttons={[
          { text: "Cancel", onClick: () => setRevoking(null), variant: variants.secondary, disabled: isRevoking },
          { text: "Revoke", onClick: handleRevokeConfirm, variant: variants.danger, loading: isRevoking },
        ]}
      >
        <div className="text-300 text-text-primary-70">
          The {revoking ? formatRole(revoking.roleName) : ""} invitation code will stop working immediately.
        </div>
      </Dialog>
    </>
  );
};

export default TeamInvitations;
