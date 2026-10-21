import { useCallback, useEffect, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";

import { ACCOUNT_READ_TIMEOUT_MS } from "./api";
import { accountPath, roleLabel } from "./capabilities";
import { AccountState } from "./components/AccountState";
import { InvitationCodeInput } from "./components/InvitationCodeInput";
import { useHostedAccounts } from "./HostedAccountsContext";
import {
  emptyInvitationCode,
  formatInvitationCode,
  INVITATION_CODE_FORMAT,
  INVITATION_CODE_LENGTH,
  invitationCodeFrom,
  isCompleteInvitationCode,
  normalizeInvitationCode,
} from "./invitationCode";
import { useAccountQuery } from "./useAccountQuery";
import { invitationClient } from "@/protoFleet/api/clients";
import { AuthenticateRequestSchema } from "@/protoFleet/api/generated/auth/v1/auth_pb";
import type {
  AcceptInvitationResponse,
  PreviewInvitationResponse,
  RegisterInvitedUserResponse,
} from "@/protoFleet/api/generated/invitation/v1/invitation_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useLogin } from "@/protoFleet/api/useLogin";
import { useLogoutAction } from "@/protoFleet/api/useLogout";
import Footer from "@/protoFleet/components/Footer";
import LoginForm from "@/protoFleet/features/auth/components/LoginModal/LoginForm";
import { formatRole } from "@/protoFleet/features/settings/utils/formatRole";
import { useFleetStore, useSetPermissions, useSetRole } from "@/protoFleet/store";
import { Alert, LogoAlt } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Header from "@/shared/components/Header";
import Input from "@/shared/components/Input";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { TabStrip, TabStripItem } from "@/shared/components/Tab";

type Destination = { kind: "account"; accountId: string } | { kind: "team" };

const destinationOf = ({ target }: AcceptInvitationResponse | RegisterInvitedUserResponse): Destination => {
  if (target.case === "account") return { kind: "account", accountId: target.value.accountId };
  if (target.case === "team") return { kind: "team" };
  throw new Error("Invitation result is missing its destination");
};
const destinationPath = (destination: Destination) =>
  destination.kind === "account" ? accountPath(destination.accountId) : "/";

const invitationHeader = ({ target }: PreviewInvitationResponse) => {
  // Organization names are generated identifiers, so team previews name only the role.
  if (target.case === "team")
    return { title: "Join the fleet team", description: `You’re invited as ${formatRole(target.value.roleName)}.` };
  if (target.case === "account")
    return {
      title: `Join ${target.value.accountName}`,
      description: `You’re invited as a ${roleLabel(target.value.role)}.`,
    };
  return null;
};

const InvitationDetails = ({ token, onCancel }: { token: string; onCancel: () => void }) => {
  const navigate = useNavigate(),
    login = useLogin(),
    logout = useLogoutAction(null);
  const authenticated = useFleetStore((s) => s.auth.isAuthenticated);
  const username = useFleetStore((s) => s.auth.username);
  const setRole = useSetRole();
  const setPermissions = useSetPermissions();
  const directory = useHostedAccounts();
  const [lifetime] = useState(() => ({ controller: new AbortController() }));
  useEffect(() => {
    if (lifetime.controller.signal.aborted) lifetime.controller = new AbortController();
    return () => lifetime.controller.abort();
  }, [lifetime]);
  const [registered, setRegistered] = useState<Destination | null>(null);
  const [needsPasswordChange, setNeedsPasswordChange] = useState(false);
  const [tab, setTab] = useState("new"),
    [newUsername, setNewUsername] = useState(""),
    [password, setPassword] = useState(""),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const load = useCallback(
    (signal: AbortSignal) =>
      invitationClient.previewInvitation({ token }, { timeoutMs: ACCOUNT_READ_TIMEOUT_MS, signal }),
    [token],
  );
  const query = useAccountQuery(load);
  const header = query.data ? invitationHeader(query.data) : null;
  // Expired, revoked, accepted and unknown codes are indistinguishable by design.
  const unusable = query.errorCode === Code.FailedPrecondition;
  const showingLogin = query.data && (!authenticated || needsPasswordChange) && tab === "existing";
  const cancel = (
    <Button variant="textOnly" text={unusable ? "Enter another code" : "Cancel"} disabled={busy} onClick={onCancel} />
  );
  const accept = async () => {
    const generation = useFleetStore.getState().auth.sessionGeneration;
    const signal = lifetime.controller.signal;
    if (busy || signal.aborted) return;
    setBusy(true);
    setError("");
    try {
      let destination = registered;
      if (!destination) {
        const response = await invitationClient.acceptInvitation({ token }, { signal });
        if (signal.aborted) return;
        destination = destinationOf(response);
        if (response.target.case === "team" && response.target.value.userInfo) {
          setRole(response.target.value.userInfo.role);
          setPermissions(response.target.value.userInfo.permissions);
          directory?.refresh();
        }
      }
      if (!signal.aborted) navigate(destinationPath(destination), { replace: true });
    } catch (e) {
      if (!signal.aborted) {
        setError(getErrorMessage(e));
        if (
          ConnectError.from(e).code === Code.Unauthenticated &&
          useFleetStore.getState().auth.sessionGeneration === generation
        )
          useFleetStore.getState().auth.logout();
      }
    } finally {
      if (!signal.aborted) setBusy(false);
    }
  };
  // Signing out keeps the previewed invitation so another login can redeem it.
  const switchLogin = async () => {
    if (busy) return;
    setBusy(true);
    setError("");
    await logout();
    if (lifetime.controller.signal.aborted) return;
    setTab("new");
    setBusy(false);
  };
  const register = async () => {
    const signal = lifetime.controller.signal;
    if (busy || signal.aborted) return;
    setBusy(true);
    setError("");
    try {
      const result = await invitationClient.registerInvitedUser(
        { token, username: newUsername.trim(), password },
        { signal },
      );
      if (signal.aborted) return;
      const destination = destinationOf(result);
      setRegistered(destination);
      await login({
        signal,
        loginRequest: create(AuthenticateRequestSchema, { username: newUsername.trim(), password }),
        onSuccess: () => navigate(destinationPath(destination), { replace: true }),
        onError: (message) => {
          setError(message);
          setTab("existing");
        },
      });
    } catch (e) {
      if (!signal.aborted) setError(getErrorMessage(e));
    } finally {
      if (!signal.aborted) setBusy(false);
      if (!signal.aborted) setPassword("");
    }
  };
  return (
    <>
      {unusable ? (
        <Header
          title="This invitation can’t be used"
          titleSize="text-heading-300"
          description="It may have expired, been revoked, or already been used. Ask the sender for a new code."
        />
      ) : (
        <AccountState loading={query.loading} error={query.error} retry={query.refresh} />
      )}
      {header ? (
        <>
          <Header title={header.title} titleSize="text-heading-300" description={header.description} />
          {authenticated && !needsPasswordChange ? (
            <>
              <p className="text-300 text-text-primary-70">Join with your current login: {username}</p>
              <Button
                variant="primary"
                text="Accept invitation"
                loading={busy}
                onClick={() => {
                  void accept();
                }}
              />
              <Button
                variant="secondary"
                text="Use a different login"
                disabled={busy}
                onClick={() => {
                  void switchLogin();
                }}
              />
            </>
          ) : (
            <>
              <TabStrip
                activeId={tab}
                onSelect={(next) => {
                  if (!busy) setTab(next);
                }}
                ariaLabel="Invitation login options"
              >
                <TabStripItem id="new" label="Create login" />
                <TabStripItem id="existing" label="Existing login" />
              </TabStrip>
              {tab === "existing" ? (
                <LoginForm
                  secondaryAction={cancel}
                  onSuccess={(requiresPasswordChange) => {
                    setNeedsPasswordChange(requiresPasswordChange);
                    if (requiresPasswordChange) {
                      setError("Change your temporary password, then enter the invitation code again.");
                    }
                  }}
                />
              ) : (
                <div className="flex flex-col gap-4">
                  <Input
                    id="invited-username"
                    label="Username"
                    initValue={newUsername}
                    onChange={setNewUsername}
                    maxLength={255}
                    autoComplete="username"
                  />
                  <Input
                    id="invited-password"
                    label="Choose a password"
                    type="password"
                    initValue={password}
                    onChange={(value) => setPassword(value.trim())}
                    maxLength={128}
                    autoComplete="new-password"
                  />
                  <Button
                    variant="primary"
                    text="Create login and join"
                    loading={busy}
                    disabled={!newUsername.trim() || !password}
                    onClick={() => {
                      void register();
                    }}
                  />
                </div>
              )}
            </>
          )}
        </>
      ) : null}
      {needsPasswordChange ? <Button variant="secondary" text="Change password" to="/update-password" /> : null}
      {error ? <Callout title={error} intent="danger" prefixIcon={<Alert />} /> : null}
      {!showingLogin ? cancel : null}
    </>
  );
};

const AccountInvitation = () => {
  const location = useLocation(),
    navigate = useNavigate();
  const [{ token, code }, setInvitation] = useState(() => {
    const initialCode = new URLSearchParams(location.hash.slice(1)).get("token") ?? "";
    const linkLength = normalizeInvitationCode(initialCode).length;
    const linkCode = invitationCodeFrom(initialCode);
    if (linkLength === INVITATION_CODE_LENGTH && isCompleteInvitationCode(linkCode))
      return { token: formatInvitationCode(linkCode), code: emptyInvitationCode() };
    // Never pre-fill a truncated prefix of an over-long link code.
    return { token: "", code: linkLength <= INVITATION_CODE_LENGTH ? linkCode : emptyInvitationCode() };
  });
  const submitCode = () => {
    if (isCompleteInvitationCode(code))
      setInvitation({ token: formatInvitationCode(code), code: emptyInvitationCode() });
  };
  useEffect(() => {
    if (location.hash) navigate(location.pathname, { replace: true });
  }, [location.hash, location.pathname, navigate]);
  return (
    <div className="flex min-h-screen flex-col bg-surface-base">
      <main className="mx-auto flex w-full max-w-120 grow flex-col justify-center gap-6 p-6" data-dd-privacy="mask">
        <LogoAlt width="w-10" />
        {token ? (
          location.hash ? (
            <div className="flex justify-center p-10">
              <ProgressCircular indeterminate />
            </div>
          ) : (
            <InvitationDetails
              key={token}
              token={token}
              onCancel={() => setInvitation({ token: "", code: emptyInvitationCode() })}
            />
          )
        ) : (
          <>
            <Header
              title="Use an invitation code"
              titleSize="text-heading-300"
              description={
                <>
                  Enter your invitation code in the format{" "}
                  <span className="whitespace-nowrap">{INVITATION_CODE_FORMAT}</span>.
                </>
              }
            />
            <InvitationCodeInput
              value={code}
              onChange={(code) => setInvitation((current) => ({ ...current, code }))}
              onSubmit={submitCode}
              autoFocus
            />
            <Button variant="primary" text="Continue" disabled={!isCompleteInvitationCode(code)} onClick={submitCode} />
            <Button variant="textOnly" text="Cancel" to="/" />
          </>
        )}
      </main>
      <Footer />
    </div>
  );
};
export default AccountInvitation;
