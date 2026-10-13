import { useCallback, useState } from "react";

import { useRequiredAccountScope } from "../AccountScope";
import { roleLabel, roleOptions } from "../capabilities";
import { useAccountQuery } from "../useAccountQuery";
import { AccountState } from "./AccountState";
import { ActionModal } from "./ActionModal";
import { PageControls } from "./PageControls";
import { ShareInvitationModal } from "./ShareInvitationModal";
import { AccountRole, type Invitation, type Member } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import Button from "@/shared/components/Button";
import List from "@/shared/components/List";
import Select from "@/shared/components/Select";
import { TabStrip, TabStripItem } from "@/shared/components/Tab";
import { formatTimestamp } from "@/shared/utils/formatTimestamp";

const invitationState = (invitation: Invitation) =>
  invitation.acceptedAt
    ? "Accepted"
    : invitation.revokedAt
      ? "Revoked"
      : invitation.expiresAt && Number(invitation.expiresAt.seconds) * 1000 <= Date.now()
        ? "Expired"
        : invitation.available
          ? "Available"
          : "Unavailable";
const AccountTeam = () => {
  const { account, client } = useRequiredAccountScope();
  const [tab, setTab] = useState("members");
  const [tokens, setTokens] = useState([""]);
  const [page, setPage] = useState(0);
  const membersLoad = useCallback(
    (signal: AbortSignal) => client.listMembers({ accountId: account.accountId }, { signal }),
    [account.accountId, client],
  );
  const invitesLoad = useCallback(
    (signal: AbortSignal) =>
      client.listInvitations({ accountId: account.accountId, pageSize: 25, pageToken: tokens[page] }, { signal }),
    [account.accountId, client, page, tokens],
  );
  const members = useAccountQuery(membersLoad, tab === "members");
  const invitations = useAccountQuery(invitesLoad, tab === "invitations");
  const [action, setAction] = useState<{
    kind: "invite" | "role" | "remove" | "revoke";
    member?: Member;
    invitation?: Invitation;
  } | null>(null);
  const [role, setRole] = useState(String(AccountRole.VIEWER));
  const [expiry, setExpiry] = useState("86400");
  const [invitationCode, setInvitationCode] = useState<string | null>(null);
  const query = tab === "members" ? members : invitations;
  const createInvite = () => {
    setRole(String(AccountRole.VIEWER));
    setExpiry("86400");
    setAction({ kind: "invite" });
  };
  return (
    <div className="flex flex-col gap-6">
      <SettingsPageHeader
        title="Team"
        description={`Manage memberships and invitations for ${account.name}. Other accounts and global login credentials are unaffected.`}
      />
      <TabStrip activeId={tab} onSelect={setTab} ariaLabel="Account team sections">
        <TabStripItem id="members" label="Members" />
        <TabStripItem id="invitations" label="Invitations" />
      </TabStrip>
      <AccountState loading={query.loading} error={query.error} retry={query.refresh} />
      {tab === "members" && members.data ? (
        <List<Member, string, "username" | "role" | "joinedAt">
          items={members.data.members}
          itemKey="userId"
          itemSelectable={false}
          activeCols={["username", "role", "joinedAt"]}
          colTitles={{ username: "Username", role: "Role", joinedAt: "Joined" }}
          colConfig={{
            username: { width: "min-w-40" },
            role: { width: "min-w-44", component: (member) => roleLabel(member.role) },
            joinedAt: {
              width: "min-w-40",
              component: (member) => formatTimestamp(member.joinedAt ? Number(member.joinedAt.seconds) : undefined),
            },
          }}
          headerControls={<Button variant="primary" text="Create invitation" onClick={createInvite} />}
          actions={[
            {
              title: "Change role",
              actionHandler: (member) => {
                setRole(String(member.role));
                setAction({ kind: "role", member });
              },
            },
            {
              title: "Remove membership",
              variant: "destructive",
              actionHandler: (member) => setAction({ kind: "remove", member }),
            },
          ]}
          noDataElement={<SettingsEmptyState title="No account members" />}
          total={members.data.members.length}
          itemName={{ singular: "member", plural: "members" }}
        />
      ) : null}
      {tab === "invitations" && invitations.data ? (
        <>
          <List<Invitation, string, "role" | "expiresAt" | "state">
            items={invitations.data.invitations}
            itemKey="invitationId"
            itemSelectable={false}
            activeCols={["role", "expiresAt", "state"]}
            colTitles={{ role: "Role", expiresAt: "Expires", state: "Status" }}
            colConfig={{
              role: { width: "min-w-40", component: (invitation) => roleLabel(invitation.role) },
              expiresAt: {
                width: "min-w-44",
                component: (invitation) =>
                  formatTimestamp(invitation.expiresAt ? Number(invitation.expiresAt.seconds) : undefined),
              },
              state: { width: "min-w-28", component: invitationState },
            }}
            headerControls={<Button variant="primary" text="Create invitation" onClick={createInvite} />}
            actions={[
              {
                title: "Revoke invitation",
                variant: "destructive",
                hidden: (invitation) => !invitation.available,
                actionHandler: (invitation) => setAction({ kind: "revoke", invitation }),
              },
            ]}
            noDataElement={
              <SettingsEmptyState
                title="No invitations on this page"
                description="Create a link and share it with the intended recipient."
              />
            }
          />
          <PageControls
            previous={page > 0}
            next={!!invitations.data.nextPageToken}
            onPrevious={() => setPage((p) => p - 1)}
            onNext={() => {
              setTokens((t) => [...t.slice(0, page + 1), invitations.data!.nextPageToken]);
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
          onDismiss={() => setAction(null)}
          onSubmit={async () => {
            if (action.kind === "invite") {
              const result = await client.createInvitation({
                accountId: account.accountId,
                role: Number(role) as AccountRole,
                expiresInSeconds: Number(expiry),
              });
              setInvitationCode(result.token);
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
            setAction(null);
            query.refresh();
          }}
        >
          {action.kind === "invite" || action.kind === "role" ? (
            <>
              <Select id="account-member-role" label="Role" value={role} options={roleOptions} onChange={setRole} />
              {action.kind === "invite" ? (
                <>
                  <Select
                    id="account-invitation-expiry"
                    label="Link expires after"
                    value={expiry}
                    onChange={setExpiry}
                    options={[
                      { value: "3600", label: "1 hour" },
                      { value: "86400", label: "24 hours" },
                      { value: "604800", label: "7 days" },
                    ]}
                  />
                  <p className="text-200 text-text-primary-50">
                    A single-use code is shown once for manual sharing. The recipient uses a Fleet username; email is
                    not required.
                  </p>
                </>
              ) : (
                <p>
                  Change {action.member!.username}’s role in this account. The server preserves administrator coverage.
                </p>
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
