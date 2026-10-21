import { useCallback, useState } from "react";
import { useSearchParams } from "react-router-dom";

import { useRequiredAccountScope } from "../AccountScope";
import { ACCOUNT_READ_TIMEOUT_MS } from "../api";
import { roleLabel, roleOptions } from "../capabilities";
import { useAccountQuery } from "../useAccountQuery";
import { AccountState } from "./AccountState";
import { ActionModal } from "./ActionModal";
import { PageControls } from "./PageControls";
import { ShareInvitationModal } from "./ShareInvitationModal";
import { AccountRole, type Invitation, type Member } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import Button, { sizes } from "@/shared/components/Button";
import List from "@/shared/components/List";
import Select from "@/shared/components/Select";
import { formatTimestamp } from "@/shared/utils/formatTimestamp";

const invitationState = (invitation: Invitation) =>
  invitation.revokedAt
    ? "Revoked"
    : invitation.expiresAt && Number(invitation.expiresAt.seconds) * 1000 <= Date.now()
      ? "Expired"
      : invitation.available
        ? "Pending"
        : "Unavailable";
type TeamEntry =
  { key: string; kind: "member"; member: Member } | { key: string; kind: "invitation"; invitation: Invitation };

const AccountTeam = () => {
  const { account, client, readOnly = false } = useRequiredAccountScope();
  const [params, setParams] = useSearchParams();
  const invite = params.get("invite");
  const [tokens, setTokens] = useState([""]);
  const [page, setPage] = useState(0);
  const load = useCallback(
    async (signal: AbortSignal) => {
      const [members, invitations] = await Promise.all([
        client.listMembers({ accountId: account.accountId }, { timeoutMs: ACCOUNT_READ_TIMEOUT_MS, signal }),
        client.listInvitations(
          { accountId: account.accountId, pageSize: 25, pageToken: tokens[page] },
          { timeoutMs: ACCOUNT_READ_TIMEOUT_MS, signal },
        ),
      ]);
      return { members: members.members, ...invitations };
    },
    [account.accountId, client, page, tokens],
  );
  const query = useAccountQuery(load);
  const entries: TeamEntry[] = query.data
    ? [
        ...(page === 0
          ? query.data.members.map((member) => ({ key: `member:${member.userId}`, kind: "member" as const, member }))
          : []),
        ...query.data.invitations.map((invitation) => ({
          key: `invitation:${invitation.invitationId}`,
          kind: "invitation" as const,
          invitation,
        })),
      ]
    : [];
  const [action, setAction] = useState<{
    kind: "invite" | "role" | "remove" | "revoke";
    member?: Member;
    invitation?: Invitation;
  } | null>(() => (!readOnly && (invite === "1" || invite === "administrator") ? { kind: "invite" } : null));
  const [role, setRole] = useState(String(invite === "administrator" ? AccountRole.ADMINISTRATOR : AccountRole.VIEWER));
  const [expiry, setExpiry] = useState("86400");
  const [invitationCode, setInvitationCode] = useState<string | null>(null);
  const closeAction = () => {
    setAction(null);
    if (params.has("invite")) {
      const next = new URLSearchParams(params);
      next.delete("invite");
      setParams(next, { replace: true });
    }
  };
  const createInvite = () => {
    setRole(String(AccountRole.VIEWER));
    setExpiry("86400");
    setAction({ kind: "invite" });
  };
  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0 flex-1">
          <SettingsPageHeader title="Team" />
        </div>
        {query.data ? (
          <Button
            disabled={readOnly}
            variant="primary"
            size={sizes.compact}
            text="Invite team member"
            onClick={createInvite}
          />
        ) : null}
      </div>
      <AccountState loading={query.loading} error={query.error} retry={query.refresh} />
      {query.data ? (
        <>
          <List<TeamEntry, string, "name" | "role" | "status" | "addedAt" | "expiresAt">
            items={entries}
            itemKey="key"
            itemSelectable={false}
            stickyFirstColumn={false}
            activeCols={["name", "role", "status", "addedAt", "expiresAt"]}
            colTitles={{ name: "Name", role: "Role", status: "Status", addedAt: "Added", expiresAt: "Expires" }}
            colConfig={{
              name: {
                width: "min-w-40",
                component: (entry) => (entry.kind === "member" ? entry.member.username : "Invitation"),
              },
              role: {
                width: "min-w-44",
                component: (entry) => roleLabel(entry.kind === "member" ? entry.member.role : entry.invitation.role),
              },
              status: {
                width: "min-w-28",
                component: (entry) => (entry.kind === "member" ? "Active" : invitationState(entry.invitation)),
              },
              addedAt: {
                width: "min-w-40",
                component: (entry) => {
                  const timestamp = entry.kind === "member" ? entry.member.joinedAt : entry.invitation.createdAt;
                  return formatTimestamp(timestamp ? Number(timestamp.seconds) : undefined);
                },
              },
              expiresAt: {
                width: "min-w-40",
                component: (entry) =>
                  formatTimestamp(
                    entry.kind === "invitation" && entry.invitation.expiresAt
                      ? Number(entry.invitation.expiresAt.seconds)
                      : undefined,
                  ),
              },
            }}
            actions={[
              {
                title: "Change role",
                disabled: readOnly,
                hidden: (entry) => entry.kind !== "member",
                actionHandler: (entry) => {
                  if (entry.kind === "member") {
                    setRole(String(entry.member.role));
                    setAction({ kind: "role", member: entry.member });
                  }
                },
              },
              {
                title: "Remove membership",
                disabled: readOnly,
                variant: "destructive",
                hidden: (entry) => entry.kind !== "member",
                actionHandler: (entry) => {
                  if (entry.kind === "member") setAction({ kind: "remove", member: entry.member });
                },
              },
              {
                title: "Revoke invitation",
                disabled: readOnly,
                variant: "destructive",
                hidden: (entry) => entry.kind !== "invitation" || invitationState(entry.invitation) !== "Pending",
                actionHandler: (entry) => {
                  if (entry.kind === "invitation") setAction({ kind: "revoke", invitation: entry.invitation });
                },
              },
            ]}
            noDataElement={
              <SettingsEmptyState
                title="No team members or invitations"
                description="Create an invitation and share its code."
              />
            }
          />
          <PageControls
            previous={page > 0}
            next={!!query.data.nextPageToken}
            onPrevious={() => setPage((p) => p - 1)}
            onNext={() => {
              setTokens((t) => [...t.slice(0, page + 1), query.data!.nextPageToken]);
              setPage((p) => p + 1);
            }}
          />
        </>
      ) : null}
      {action ? (
        <ActionModal
          title={
            action.kind === "invite"
              ? "Create invitation"
              : action.kind === "role"
                ? "Change account role"
                : action.kind === "remove"
                  ? "Remove membership"
                  : "Revoke invitation"
          }
          confirm={
            action.kind === "invite"
              ? "Create invitation"
              : action.kind === "remove"
                ? "Remove membership"
                : action.kind === "revoke"
                  ? "Revoke invitation"
                  : "Save role"
          }
          onDismiss={closeAction}
          onSubmit={async () => {
            if (action.kind === "invite") {
              const result = await client.createInvitation({
                accountId: account.accountId,
                role: Number(role) as AccountRole,
                expiresInSeconds: Number(expiry),
              });
              setInvitationCode(result.token);
              setPage(0);
              setTokens([""]);
            } else if (action.kind === "role")
              await client.setMemberRole({
                accountId: account.accountId,
                userId: action.member!.userId,
                role: Number(role) as AccountRole,
              });
            else if (action.kind === "remove")
              await client.revokeMember({ accountId: account.accountId, userId: action.member!.userId });
            else
              await client.revokeInvitation({
                accountId: account.accountId,
                invitationId: action.invitation!.invitationId,
              });
            closeAction();
            query.refresh();
          }}
        >
          {action.kind === "invite" || action.kind === "role" ? (
            <>
              <Select id="account-member-role" label="Role" value={role} options={roleOptions} onChange={setRole} />
              {action.kind === "invite" ? (
                <>
                  <p>Invite someone to {account.name}.</p>
                  <Select
                    id="account-invitation-expiry"
                    label="Expires after"
                    value={expiry}
                    onChange={setExpiry}
                    options={[
                      { value: "3600", label: "1 hour" },
                      { value: "86400", label: "24 hours" },
                    ]}
                  />
                  <p className="text-200 text-text-primary-50">You’ll receive a single-use invitation code to share.</p>
                </>
              ) : (
                <p>Change {action.member!.username}’s role in this account.</p>
              )}
            </>
          ) : action.kind === "remove" ? (
            <p>
              Remove {action.member!.username} from {account.name}? Their access and queued pool changes are revoked for
              this account. Other memberships remain active.
            </p>
          ) : (
            <p>The invitation can no longer be used to join this account.</p>
          )}
        </ActionModal>
      ) : null}
      {invitationCode ? (
        <ShareInvitationModal token={invitationCode} onDismiss={() => setInvitationCode(null)} />
      ) : null}
    </div>
  );
};
export default AccountTeam;
