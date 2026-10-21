import { useCallback, useEffect, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";

import { ACCOUNT_READ_TIMEOUT_MS, hostedAccountsClient } from "./api";
import { accountPath, roleLabel } from "./capabilities";
import { AccountState } from "./components/AccountState";
import { useAccountQuery } from "./useAccountQuery";
import { AuthenticateRequestSchema } from "@/protoFleet/api/generated/auth/v1/auth_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useLogin } from "@/protoFleet/api/useLogin";
import Footer from "@/protoFleet/components/Footer";
import LoginForm from "@/protoFleet/features/auth/components/LoginModal/LoginForm";
import { useFleetStore } from "@/protoFleet/store";
import { Alert, LogoAlt } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Header from "@/shared/components/Header";
import Input from "@/shared/components/Input";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { TabStrip, TabStripItem } from "@/shared/components/Tab";

const InvitationDetails = ({ token, onChangeCode }: { token: string; onChangeCode: () => void }) => {
  const navigate = useNavigate(),
    login = useLogin();
  const authenticated = useFleetStore((s) => s.auth.isAuthenticated);
  const username = useFleetStore((s) => s.auth.username);
  const [lifetime] = useState(() => ({ controller: new AbortController() }));
  useEffect(() => {
    if (lifetime.controller.signal.aborted) lifetime.controller = new AbortController();
    return () => lifetime.controller.abort();
  }, [lifetime]);
  const [registeredAccountId, setRegisteredAccountId] = useState("");
  const [needsPasswordChange, setNeedsPasswordChange] = useState(false);
  const [tab, setTab] = useState("existing"),
    [newUsername, setNewUsername] = useState(""),
    [password, setPassword] = useState(""),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const load = useCallback(
    (signal: AbortSignal) =>
      hostedAccountsClient.previewInvitation({ token }, { timeoutMs: ACCOUNT_READ_TIMEOUT_MS, signal }),
    [token],
  );
  const query = useAccountQuery(load);
  const accept = async () => {
    const generation = useFleetStore.getState().auth.sessionGeneration;
    const signal = lifetime.controller.signal;
    if (busy || signal.aborted) return;
    setBusy(true);
    setError("");
    try {
      const result = registeredAccountId
        ? { accountId: registeredAccountId }
        : await hostedAccountsClient.acceptInvitation({ token }, { signal });
      if (!signal.aborted) navigate(accountPath(result.accountId), { replace: true });
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
  const register = async () => {
    const signal = lifetime.controller.signal;
    if (busy || signal.aborted) return;
    setBusy(true);
    setError("");
    try {
      const result = await hostedAccountsClient.registerInvitedUser(
        { token, username: newUsername.trim(), password },
        { signal },
      );
      if (signal.aborted) return;
      setRegisteredAccountId(result.accountId);
      await login({
        signal,
        loginRequest: create(AuthenticateRequestSchema, { username: newUsername.trim(), password }),
        onSuccess: () => navigate(accountPath(result.accountId), { replace: true }),
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
      <AccountState loading={query.loading} error={query.error} retry={query.refresh} />
      {query.data ? (
        <>
          <Header
            title={`Join ${query.data.accountName}`}
            titleSize="text-heading-300"
            description={`You’re invited as a ${roleLabel(query.data.role)}.`}
          />
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
                <TabStripItem id="existing" label="Existing login" />
                <TabStripItem id="new" label="Create login" />
              </TabStrip>
              {tab === "existing" ? (
                <LoginForm
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
      <Button variant="textOnly" text="Use another invitation code" disabled={busy} onClick={onChangeCode} />
    </>
  );
};

const isInvitationCode = (value: string) =>
  /^(?:[A-HJ-NP-Z2-9]{4}-?){3}[A-HJ-NP-Z2-9]{4}$/i.test(value.replaceAll(" ", ""));
const AccountInvitation = () => {
  const location = useLocation(),
    navigate = useNavigate();
  const [{ token, code }, setInvitation] = useState(() => {
    const initialCode = new URLSearchParams(location.hash.slice(1)).get("token") ?? "";
    return isInvitationCode(initialCode) ? { token: initialCode, code: "" } : { token: "", code: initialCode };
  });
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
            <InvitationDetails key={token} token={token} onChangeCode={() => setInvitation({ token: "", code: "" })} />
          )
        ) : (
          <>
            <Header
              title="Join an account"
              titleSize="text-heading-300"
              description="Enter your invitation code, such as K7MP-4X9R-6TWC-8NQF."
            />
            <Input
              id="invitation-code"
              label="Invitation code"
              initValue={code}
              onChange={(code) => setInvitation((current) => ({ ...current, code }))}
              maxLength={128}
              autoComplete="off"
            />
            <Button
              variant="primary"
              text="Continue"
              disabled={!isInvitationCode(code)}
              onClick={() => {
                setInvitation({ token: code, code: "" });
              }}
            />
          </>
        )}
        <Button variant="textOnly" text="Back to Fleet" to="/" />
      </main>
      <Footer />
    </div>
  );
};
export default AccountInvitation;
