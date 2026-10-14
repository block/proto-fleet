import { useState } from "react";

import { useRequiredAccountScope } from "../AccountScope";
import { can, capabilities, roleLabel } from "../capabilities";
import { ActionModal } from "./ActionModal";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import Button from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Row from "@/shared/components/Row";

const AccountSettings = ({ onRenamed }: { onRenamed: () => void }) => {
  const { account, client } = useRequiredAccountScope();
  const [renaming, setRenaming] = useState(false);
  const [name, setName] = useState(account.name);
  return (
    <div className="flex flex-col gap-6">
      <SettingsPageHeader title="Settings" />
      <div>
        <Row className="flex items-center justify-between gap-4">
          Your access
          <span className="ml-auto text-text-primary-70">
            {account.depth === 0 ? "Host administration" : roleLabel(account.role)}
          </span>
        </Row>
        <Row className="flex items-center justify-between gap-4">
          Status<span className="ml-auto">{account.suspended ? "Suspended" : "Active"}</span>
        </Row>
      </div>
      <div>
        <div className="mb-3 text-heading-200 text-text-primary">Permissions</div>
        {capabilities
          .filter((c) => can(account, c.value))
          .map((c) => (
            <Row key={String(c.value)}>{c.label}</Row>
          ))}
      </div>
      {can(account, 8n) ? (
        <Button
          variant="secondary"
          className="self-start"
          text="Rename account"
          onClick={() => {
            setName(account.name);
            setRenaming(true);
          }}
        />
      ) : null}
      {renaming ? (
        <ActionModal
          title="Rename account"
          valid={!!name.trim()}
          onDismiss={() => setRenaming(false)}
          onSubmit={async () => {
            await client.renameAccount({ accountId: account.accountId, name: name.trim() });
            setRenaming(false);
            onRenamed();
          }}
        >
          <Input id="hosted-account-name" label="Account name" initValue={name} onChange={setName} maxLength={255} />
        </ActionModal>
      ) : null}
    </div>
  );
};
export default AccountSettings;
