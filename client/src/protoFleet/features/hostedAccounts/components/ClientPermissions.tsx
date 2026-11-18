import { useState } from "react";

import { useRequiredAccountScope } from "../AccountScope";
import { ticketingDescription } from "../capabilities";
import { useAccountMutation } from "../useAccountMutation";
import { CapabilityFields } from "./CapabilityFields";
import type { Account } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import { Alert } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Checkbox from "@/shared/components/Checkbox";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";

export const ClientPermissions = ({ child, onSaved }: { child: Account; onSaved: () => void }) => {
  const scope = useRequiredAccountScope();
  const { account, client, readOnly } = scope;
  const allowed = account.depth === 0 ? account.capabilityCeiling : account.effectiveCapabilities;
  const required = 9n;
  const [ceiling, setCeiling] = useState((child.capabilityCeiling & allowed) | required);
  const [ticketingEnabled, setTicketingEnabled] = useState(child.ticketingEnabled);
  const [suspended, setSuspended] = useState(child.suspended);
  const [confirming, setConfirming] = useState(false);
  const { run, busy, error, clearError } = useAccountMutation();
  const configure = async ({
    capabilities = ceiling,
    suspended: nextSuspended = suspended,
    ticketingEnabled: nextTicketing = ticketingEnabled,
  }: {
    capabilities?: bigint;
    suspended?: boolean;
    ticketingEnabled?: boolean;
  }) => {
    await client.configureAccount({
      accountId: account.accountId,
      childAccountId: child.accountId,
      capabilities,
      suspended: nextSuspended,
      ticketingEnabled: nextTicketing,
    });
  };
  const changePermissions = async (next: bigint) => {
    if (busy || readOnly || (next & required) !== required || (next & ~allowed) !== 0n) return;
    const previous = ceiling;
    setCeiling(next);
    const saved = await run(() => configure({ capabilities: next }));
    if (scope.signal.aborted) return;
    if (saved) onSaved();
    else setCeiling(previous);
  };
  const dismissConfirmation = () => {
    clearError();
    setConfirming(false);
  };
  const confirmSuspension = async () => {
    const nextSuspended = !suspended;
    const saved = await run(() => configure({ suspended: nextSuspended }));
    if (saved) {
      setSuspended(nextSuspended);
      setConfirming(false);
      onSaved();
    }
  };
  const action = suspended ? "Resume account" : "Suspend account";
  return (
    <section className="flex flex-col gap-6" aria-label="Client permissions">
      <h2 className="text-heading-200 text-text-primary">Client permissions</h2>
      <fieldset disabled={busy || readOnly || confirming}>
        <CapabilityFields
          value={ceiling}
          allowed={allowed}
          required={required}
          onChange={(next) => {
            void changePermissions(next);
          }}
        />
        <label className="mt-4 flex items-start gap-3 text-300 text-text-primary">
          <Checkbox
            ariaLabel="Ticketing"
            className="shrink-0"
            checked={ticketingEnabled}
            onChange={(event) => {
              const next = event.target.checked;
              setTicketingEnabled(next);
              void run(() => configure({ ticketingEnabled: next })).then((saved) => {
                if (scope.signal.aborted) return;
                if (saved) onSaved();
                else setTicketingEnabled(!next);
              });
            }}
          />
          <span>
            Ticketing
            <span className="mt-1 block text-200 text-text-primary-50">{ticketingDescription}</span>
          </span>
        </label>
      </fieldset>
      {busy && !confirming ? (
        <p role="status" className="text-200 text-text-primary-50">
          Saving…
        </p>
      ) : null}
      {error && !confirming ? <Callout title={error} intent="danger" prefixIcon={<Alert />} /> : null}
      <div className="flex flex-wrap items-center justify-between gap-4 border-t border-border-5 pt-6">
        <div>
          <h3 className="text-heading-100 text-text-primary">Account access</h3>
          <p className="mt-1 text-300 text-text-primary-70">
            {suspended
              ? "This account and its subclients are suspended."
              : "Suspend access for this account and its subclients."}
          </p>
        </div>
        <Button
          text={action}
          variant={suspended ? variants.secondary : variants.secondaryDanger}
          size={sizes.compact}
          disabled={busy || readOnly}
          onClick={() => {
            clearError();
            setConfirming(true);
          }}
        />
      </div>
      {confirming ? (
        <Dialog
          open
          manageFocus
          title={`${suspended ? "Resume" : "Suspend"} ${child.name}?`}
          icon={
            <DialogIcon intent={suspended ? "info" : "critical"}>
              <Alert />
            </DialogIcon>
          }
          onDismiss={busy ? undefined : dismissConfirmation}
          buttons={[
            { text: "Cancel", variant: variants.secondary, disabled: busy, onClick: dismissConfirmation },
            {
              text: action,
              variant: suspended ? variants.primary : variants.danger,
              loading: busy,
              disabled: readOnly,
              onClick: () => {
                void confirmSuspension();
              },
            },
          ]}
        >
          <div className="flex flex-col gap-4">
            <p className="text-300 text-text-primary-70">
              {suspended
                ? `Restore access for ${child.name} and its subclients?`
                : `Suspend access for ${child.name} and its subclients? Miner pools stay unchanged.`}
            </p>
            {error ? <Callout title={error} intent="danger" prefixIcon={<Alert />} /> : null}
          </div>
        </Dialog>
      ) : null}
    </section>
  );
};
