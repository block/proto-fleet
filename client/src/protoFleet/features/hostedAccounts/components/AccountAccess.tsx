import { useCallback, useState } from "react";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";

import { useRequiredAccountScope } from "../AccountScope";
import { can, capabilities } from "../capabilities";
import { useHostedAccounts } from "../HostedAccountsContext";
import { useAccountQuery } from "../useAccountQuery";
import { AccountState } from "./AccountState";
import { ActionModal } from "./ActionModal";
import { CapabilityFields } from "./CapabilityFields";
import { PageControls } from "./PageControls";
import { SecretModal } from "./SecretModal";
import type { AccountKey, AncestorGrant } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import Button, { sizes } from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import Input from "@/shared/components/Input";
import List from "@/shared/components/List";
import Select from "@/shared/components/Select";
import { formatTimestamp } from "@/shared/utils/formatTimestamp";

const AccountAccess = () => {
  const { account, client } = useRequiredAccountScope();
  const directory = useHostedAccounts();
  const [tokens, setTokens] = useState([""]),
    [page, setPage] = useState(0);
  const loadKeys = useCallback(
    (signal: AbortSignal) =>
      client.listAccountKeys({ accountId: account.accountId, pageSize: 25, pageToken: tokens[page] }, { signal }),
    [account.accountId, client, tokens, page],
  );
  const loadGrants = useCallback(
    (signal: AbortSignal) => client.listAncestorGrants({ accountId: account.accountId }, { signal }),
    [account.accountId, client],
  );
  const keys = useAccountQuery(loadKeys, account.depth > 0);
  const grants = useAccountQuery(loadGrants, account.depth > 0 && can(account, 8n));
  const [keyModal, setKeyModal] = useState(false),
    [name, setName] = useState(""),
    [keyCapabilities, setKeyCapabilities] = useState(1n),
    [days, setDays] = useState("30"),
    [selectedChildren, setSelectedChildren] = useState<string[]>([]),
    [future, setFuture] = useState(false);
  const [secret, setSecret] = useState<string | null>(null),
    [revoking, setRevoking] = useState<AccountKey | null>(null);
  const [grant, setGrant] = useState<{ ancestorId: string; capabilities: bigint } | null>(null),
    [removingGrant, setRemovingGrant] = useState<AncestorGrant | null>(null);
  const ancestors = [
    ...new Set([account.parentAccountId, ...(grants.data?.grants.map((g) => g.ancestorAccountId) ?? [])]),
  ].filter(Boolean);
  const ancestorName = (id: string) =>
    directory?.accounts.find((a) => a.accountId === id)?.name ??
    (id === account.parentAccountId
      ? account.depth === 1
        ? "Hosting provider"
        : "Parent account"
      : "Ancestor account");
  const eligibleChildren =
    directory?.accounts.filter((a) => a.parentAccountId === account.accountId && can(a, 1n)) ?? [];
  return (
    <div className="flex flex-col gap-6">
      <SettingsPageHeader title="Access" />
      {can(account, 8n) && account.depth > 0 ? (
        <section className="flex flex-col gap-4">
          <h2 className="text-heading-200 text-text-primary">Ancestor access</h2>
          <p className="text-300 text-text-primary-70">Hosting alone doesn’t grant account access.</p>
          <AccountState loading={grants.loading} error={grants.error} retry={grants.refresh} />
          {grants.data ? (
            <List<AncestorGrant, string, "ancestor" | "permissions">
              items={grants.data.grants}
              itemKey="ancestorAccountId"
              itemSelectable={false}
              stickyFirstColumn={false}
              activeCols={["ancestor", "permissions"]}
              colTitles={{ ancestor: "Ancestor", permissions: "Granted permissions" }}
              colConfig={{
                ancestor: { width: "min-w-40", component: (g) => ancestorName(g.ancestorAccountId) },
                permissions: {
                  width: "min-w-64",
                  allowWrap: true,
                  component: (g) =>
                    capabilities
                      .filter((c) => (g.capabilities & c.value) !== 0n)
                      .map((c) => c.label)
                      .join(", "),
                },
              }}
              headerControls={
                <Button
                  variant="secondary"
                  size={sizes.compact}
                  text="Grant ancestor access"
                  onClick={() => setGrant({ ancestorId: account.parentAccountId, capabilities: 1n })}
                />
              }
              actions={[
                {
                  title: "Edit access",
                  actionHandler: (g) => setGrant({ ancestorId: g.ancestorAccountId, capabilities: g.capabilities }),
                },
                { title: "Revoke access", variant: "destructive", actionHandler: setRemovingGrant },
              ]}
              noDataElement={<SettingsEmptyState size="section" title="No ancestor access granted" />}
            />
          ) : null}
        </section>
      ) : null}
      {account.depth > 0 ? (
        <section className="flex flex-col gap-4">
          <h2 className="text-heading-200 text-text-primary">API keys</h2>
          <p className="text-300 text-text-primary-70">Keys are limited by their issuer’s permissions.</p>
          <AccountState loading={keys.loading} error={keys.error} retry={keys.refresh} />
          {keys.data ? (
            <>
              <List<AccountKey, string, "name" | "prefix" | "issuer" | "scope" | "expires">
                items={keys.data.keys}
                itemKey="keyId"
                itemSelectable={false}
                stickyFirstColumn={false}
                activeCols={["name", "prefix", "issuer", "scope", "expires"]}
                colTitles={{ name: "Name", prefix: "Prefix", issuer: "Issued by", scope: "Access", expires: "Expires" }}
                colConfig={{
                  name: { width: "min-w-32" },
                  prefix: { width: "min-w-28" },
                  issuer: { width: "min-w-32", component: (key) => key.issuerUsername },
                  scope: {
                    width: "min-w-40",
                    component: (key) =>
                      key.includeFutureDescendants
                        ? "Current and future subclients"
                        : key.descendantAccountIds.length
                          ? `${key.descendantAccountIds.length} selected subclients`
                          : "This account only",
                  },
                  expires: {
                    width: "min-w-40",
                    component: (key) => (key.expiresAt ? formatTimestamp(Number(key.expiresAt.seconds)) : "No expiry"),
                  },
                }}
                headerControls={
                  <Button
                    variant="primary"
                    size={sizes.compact}
                    text="Create API key"
                    onClick={() => {
                      setName("");
                      setKeyCapabilities(1n);
                      setDays("30");
                      setSelectedChildren([]);
                      setFuture(false);
                      setKeyModal(true);
                    }}
                  />
                }
                actions={[{ title: "Revoke key", variant: "destructive", actionHandler: setRevoking }]}
                noDataElement={<SettingsEmptyState title="No account keys on this page" />}
              />
              <PageControls
                previous={page > 0}
                next={!!keys.data.nextPageToken}
                onPrevious={() => setPage((p) => p - 1)}
                onNext={() => {
                  setTokens((t) => [...t.slice(0, page + 1), keys.data!.nextPageToken]);
                  setPage((p) => p + 1);
                }}
              />
            </>
          ) : null}
        </section>
      ) : null}
      {keyModal ? (
        <ActionModal
          title="Create API key"
          confirm="Create key"
          valid={!!name.trim()}
          onDismiss={() => setKeyModal(false)}
          onSubmit={async () => {
            const result = await client.createAccountKey({
              accountId: account.accountId,
              name: name.trim(),
              capabilities: keyCapabilities,
              descendantAccountIds: selectedChildren,
              includeFutureDescendants: future,
              expiresAt: timestampFromDate(new Date(Date.now() + Number(days) * 86400 * 1000)),
            });
            setSecret(result.secret);
            setKeyModal(false);
            keys.refresh();
          }}
        >
          <Input id="hosted-key-name" label="Key name" initValue={name} onChange={setName} maxLength={255} />
          <div className="flex flex-col gap-3">
            {capabilities.slice(0, 3).map((c) => (
              <label key={String(c.value)} className="flex items-center gap-3">
                <Checkbox
                  checked={(keyCapabilities & c.value) !== 0n}
                  disabled={c.value === 1n || !can(account, c.value)}
                  onChange={(e) => setKeyCapabilities((mask) => (e.target.checked ? mask | c.value : mask & ~c.value))}
                />
                {c.label}
              </label>
            ))}
          </div>
          <Select
            id="hosted-key-expiry"
            label="Expires after"
            value={days}
            onChange={setDays}
            options={[
              { value: "7", label: "7 days" },
              { value: "30", label: "30 days" },
              { value: "90", label: "90 days" },
            ]}
          />
          {account.depth < 2 ? (
            <>
              <p className="text-heading-100">Subclients</p>
              {eligibleChildren.map((child) => (
                <label key={child.accountId} className="flex items-center gap-3">
                  <Checkbox
                    checked={selectedChildren.includes(child.accountId)}
                    onChange={(e) =>
                      setSelectedChildren((ids) =>
                        e.target.checked ? [...ids, child.accountId] : ids.filter((id) => id !== child.accountId),
                      )
                    }
                  />
                  {child.name}
                </label>
              ))}
              <label className="flex items-start gap-3">
                <Checkbox checked={future} onChange={(e) => setFuture(e.target.checked)} />
                <span>
                  Include all current and future subclients
                  <p className="text-200 text-text-primary-50">The key only works for subclients you can access.</p>
                </span>
              </label>
            </>
          ) : null}
        </ActionModal>
      ) : null}
      {secret ? (
        <SecretModal
          title="API key created"
          label="Secret"
          value={secret}
          description="Copy this secret now. You won’t see it again."
          onDismiss={() => setSecret(null)}
        />
      ) : null}
      {revoking ? (
        <ActionModal
          title="Revoke API key"
          confirm="Revoke key"
          onDismiss={() => setRevoking(null)}
          onSubmit={async () => {
            await client.revokeAccountKey({ accountId: account.accountId, keyId: revoking.keyId });
            setRevoking(null);
            keys.refresh();
          }}
        >
          <p>Revoke {revoking.name}? Its subsequent requests will be denied.</p>
        </ActionModal>
      ) : null}
      {grant && can(account, 8n) ? (
        <ActionModal
          title="Ancestor access"
          onDismiss={() => setGrant(null)}
          onSubmit={async () => {
            await client.setAncestorGrant({
              accountId: account.accountId,
              ancestorAccountId: grant.ancestorId,
              capabilities: grant.capabilities,
            });
            setGrant(null);
            grants.refresh();
          }}
        >
          <Select
            id="hosted-ancestor"
            label="Ancestor account"
            value={grant.ancestorId}
            onChange={(ancestorId) => setGrant((g) => ({ ...g!, ancestorId }))}
            options={ancestors.map((id) => ({ value: id, label: ancestorName(id) }))}
          />
          <CapabilityFields
            value={grant.capabilities}
            allowed={account.effectiveCapabilities}
            onChange={(mask) => setGrant((g) => ({ ...g!, capabilities: mask }))}
          />
        </ActionModal>
      ) : null}
      {removingGrant && can(account, 8n) ? (
        <ActionModal
          title="Revoke ancestor access"
          confirm="Revoke access"
          onDismiss={() => setRemovingGrant(null)}
          onSubmit={async () => {
            await client.revokeAncestorGrant({
              accountId: account.accountId,
              ancestorAccountId: removingGrant.ancestorAccountId,
            });
            setRemovingGrant(null);
            grants.refresh();
          }}
        >
          <p>
            Remove {ancestorName(removingGrant.ancestorAccountId)}’s explicit access to this account? Host physical
            operations remain available.
          </p>
        </ActionModal>
      ) : null}
    </div>
  );
};
export default AccountAccess;
