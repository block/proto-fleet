import { useState } from "react";

import { useRequiredAccountScope } from "../AccountScope";
import { ActionModal } from "./ActionModal";
import { CapabilityFields } from "./CapabilityFields";
import type { Account } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import Checkbox from "@/shared/components/Checkbox";

export const AccountPermissionsEditor = ({
  child,
  onDismiss,
  onSaved,
}: {
  child: Account;
  onDismiss: () => void;
  onSaved: () => void;
}) => {
  const { account, client } = useRequiredAccountScope();
  const allowed = account.depth === 0 ? account.capabilityCeiling : account.effectiveCapabilities;
  const required = 9n;
  const [ceiling, setCeiling] = useState((child.capabilityCeiling & allowed) | required);
  const [ticketingEnabled, setTicketingEnabled] = useState(child.ticketingEnabled);
  const [suspended, setSuspended] = useState(child.suspended);
  return (
    <ActionModal
      title={`Manage ${child.name}`}
      confirm="Save permissions"
      valid={(ceiling & required) === required ? (ceiling & ~allowed) === 0n : false}
      onDismiss={() => {
        setCeiling((child.capabilityCeiling & allowed) | required);
        setSuspended(child.suspended);
        onDismiss();
      }}
      onSubmit={async () => {
        await client.configureAccount({
          accountId: account.accountId,
          childAccountId: child.accountId,
          capabilities: ceiling,
          suspended,
          ticketingEnabled,
        });
        onSaved();
        onDismiss();
      }}
    >
      <CapabilityFields value={ceiling} allowed={allowed} required={required} onChange={setCeiling} />
      <label className="flex items-center gap-3">
        <Checkbox checked={ticketingEnabled} onChange={(event) => setTicketingEnabled(event.target.checked)} />
        Ticketing
      </label>
      <p className="text-200 text-text-primary-50">Viewing data and managing the team are required.</p>
      <label className="flex items-start gap-3">
        <Checkbox checked={suspended} onChange={(event) => setSuspended(event.target.checked)} />
        <span>
          Suspend account
          <p className="text-200 text-text-primary-50">
            Blocks this account and its subclients. Miner pools stay unchanged.
          </p>
        </span>
      </label>
    </ActionModal>
  );
};
