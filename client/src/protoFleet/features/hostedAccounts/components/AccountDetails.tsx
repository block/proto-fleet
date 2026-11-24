import { useState } from "react";

import { useRequiredAccountScope } from "../AccountScope";
import { can, capabilities, roleLabel } from "../capabilities";
import { ActionModal } from "./ActionModal";
import Button, { sizes } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Row from "@/shared/components/Row";

const AccountDetails = () => {
  const { account, client, refreshAccount, readOnly = false } = useRequiredAccountScope();
  const [renaming, setRenaming] = useState(false);
  const [name, setName] = useState(account.name);
  return (
    <>
      <section aria-label="Account details">
        <h2 className="mb-3 text-heading-200 text-text-primary">Account details</h2>
        <Row className="flex flex-wrap items-center gap-4">
          <span>Account name</span>
          <span className="ml-auto min-w-0 break-words">{account.name}</span>
          {can(account, 8n) ? (
            <Button
              variant="secondary"
              size={sizes.compact}
              className="shrink-0"
              disabled={readOnly}
              text="Rename"
              ariaLabel="Rename account"
              onClick={() => {
                setName(account.name);
                setRenaming(true);
              }}
            />
          ) : null}
        </Row>
        <Row className="flex items-center justify-between gap-4">
          Your access
          <span className="ml-auto text-text-primary-70">
            {account.depth === 0 || readOnly ? "Host administration" : roleLabel(account.role)}
          </span>
        </Row>
        {account.suspended ? (
          <Row className="flex items-center justify-between gap-4">
            Status<span className="ml-auto">Suspended</span>
          </Row>
        ) : null}
      </section>
      <section aria-label="Your permissions">
        <h2 className="mb-3 text-heading-200 text-text-primary">Your permissions</h2>
        <ul className="flex flex-wrap gap-x-6 gap-y-1 text-300 text-text-primary-70">
          {capabilities
            .filter((c) => can(account, c.value))
            .map((c) => (
              <li key={String(c.value)}>{c.label}</li>
            ))}
        </ul>
      </section>
      {renaming ? (
        <ActionModal
          title="Rename account"
          valid={!!name.trim()}
          onDismiss={() => setRenaming(false)}
          onSubmit={async () => {
            await client.renameAccount({ accountId: account.accountId, name: name.trim() });
            setRenaming(false);
            refreshAccount();
          }}
        >
          <Input id="hosted-account-name" label="Account name" initValue={name} onChange={setName} maxLength={255} />
        </ActionModal>
      ) : null}
    </>
  );
};
export default AccountDetails;
