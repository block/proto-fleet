import { useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";

import { accountPath, roleLabel } from "@/protoFleet/features/hostedAccounts/capabilities";
import { useHostedAccounts } from "@/protoFleet/features/hostedAccounts/HostedAccountsContext";
import { ChevronDown } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";
import Radio from "@/shared/components/Radio";

const AccountPicker = () => {
  const directory = useHostedAccounts();
  const [open, setOpen] = useState(false);
  const navigate = useNavigate();
  const { pathname } = useLocation();
  if (!directory || directory.loading || directory.error || directory.accounts.length < 2) return null;
  const selected = directory.accounts.find(
    (a) => pathname === `/accounts/${a.accountId}` || pathname.startsWith(`/accounts/${a.accountId}/`),
  );
  if (directory.hostAccess) return null;
  return (
    <>
      <button
        type="button"
        className="flex max-w-full min-w-0 items-center gap-1 rounded-md px-2 py-1 text-300 text-text-primary hover:bg-surface-base-hover focus-visible:underline"
        onClick={() => setOpen(true)}
        aria-label="Switch account"
        aria-haspopup="dialog"
        aria-expanded={open}
        data-testid="account-picker"
      >
        <span className="min-w-0 truncate">{selected?.name ?? "Accounts"}</span>
        <ChevronDown width="w-3" className="shrink-0 opacity-70" />
      </button>
      <Modal
        open={open}
        title="Switch account"
        dialogLabel="Switch account"
        onDismiss={() => setOpen(false)}
        manageFocus
        divider={false}
        buttons={[
          {
            variant: variants.secondary,
            text: "Use an invitation code",
            onClick: () => {
              setOpen(false);
              navigate("/account-invitation");
            },
          },
        ]}
      >
        <div className="flex flex-col gap-2" role="radiogroup" aria-label="Accounts">
          {directory.accounts.map((account) => (
            <label
              key={account.accountId}
              className="flex cursor-pointer items-center gap-3 rounded-xl p-3 text-left hover:bg-core-primary-5"
            >
              <Radio
                name="account-workspace"
                disabled={account.suspended}
                selected={selected?.accountId === account.accountId}
                onChange={() => {
                  setOpen(false);
                  navigate(accountPath(account.accountId));
                }}
              />
              <div>
                {account.name}
                <div className="text-200 text-text-primary-50">
                  {account.suspended ? "Suspended" : roleLabel(account.role)}
                </div>
              </div>
            </label>
          ))}
        </div>
      </Modal>
    </>
  );
};
export default AccountPicker;
