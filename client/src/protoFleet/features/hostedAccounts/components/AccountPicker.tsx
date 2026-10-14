import { useState } from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";

import { accountPath, roleLabel } from "@/protoFleet/features/hostedAccounts/capabilities";
import { useHostedAccounts } from "@/protoFleet/features/hostedAccounts/HostedAccountsContext";
import { ChevronDown } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";
import Radio from "@/shared/components/Radio";

const AccountPicker = () => {
  const directory = useHostedAccounts();
  const [open, setOpen] = useState(false);
  const navigate = useNavigate();
  const { pathname } = useLocation();
  if (!directory || directory.loading || directory.error || !directory.accounts.length) return null;
  const selected = directory.accounts.find(
    (a) => pathname === `/accounts/${a.accountId}` || pathname.startsWith(`/accounts/${a.accountId}/`),
  );
  return (
    <>
      <Button
        variant="textOnly"
        textColor="text-text-primary"
        borderColor="border-border-primary"
        text={selected?.name ?? (directory.hostAccess ? "Fleet" : "Accounts")}
        suffixIcon={<ChevronDown width="w-3" />}
        textOnlyUnderlineOnHover={false}
        onClick={() => setOpen(true)}
        ariaLabel="Switch account"
        ariaHasPopup="dialog"
        ariaExpanded={open}
        testId="account-picker"
      />
      <Modal
        open={open}
        title="Switch account"
        dialogLabel="Switch account"
        onDismiss={() => setOpen(false)}
        manageFocus
      >
        <div className="flex flex-col gap-2">
          {directory.hostAccess ? (
            <label className="flex cursor-pointer items-center gap-3 rounded-xl p-3 text-left hover:bg-core-primary-5">
              <Radio
                name="account-workspace"
                selected={!selected}
                onChange={() => {
                  setOpen(false);
                  navigate("/dashboard");
                }}
              />
              <div>Fleet</div>
            </label>
          ) : null}
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
                  {account.suspended
                    ? "Suspended"
                    : account.depth === 0
                      ? "Customer administration"
                      : roleLabel(account.role)}
                </div>
              </div>
            </label>
          ))}
        </div>
        <Link
          className="mt-4 inline-block text-text-primary hover:underline"
          to="/accounts"
          onClick={() => setOpen(false)}
        >
          All accounts
        </Link>
      </Modal>
    </>
  );
};
export default AccountPicker;
