import { useCallback, useEffect, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";

import { hostedAccountsClient } from "./api";
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
import { TabStrip, TabStripItem } from "@/shared/components/Tab";

const AccountInvitation = () => {
  const location = useLocation(),
    navigate = useNavigate(),
    login = useLogin();
  const authenticated = useFleetStore((s) => s.auth.isAuthenticated);
  const username = useFleetStore((s) => s.auth.username);
  const [token] = useState(() => new URLSearchParams(location.hash.slice(1)).get("token") ?? "");
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
  useEffect(() => {
    if (location.hash) navigate(location.pathname, { replace: true });
  }, [location.hash, location.pathname, navigate]);
  const load = useCallback(
    (signal: AbortSignal) => hostedAccountsClient.previewInvitation({ token }, { signal }),
    [token],
  );
  const query = useAccountQuery(load, token.length === 43);
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
    <div className="flex min-h-screen flex-col bg-surface-base">
      <main className="mx-auto flex w-full max-w-120 grow flex-col justify-center gap-6 p-6" data-dd-privacy="mask">
        <LogoAlt width="w-10" />
        {token.length !== 43 ? (
          <Callout
            title="This invitation link is incomplete. Ask the account administrator for a new link."
            intent="danger"
            prefixIcon={<Alert />}
          />
        ) : (
          <>
            <AccountState loading={query.loading} error={query.error} retry={query.refresh} />
            {query.data ? (
              <>
                <Header
                  title={`Join ${query.data.accountName}`}
                  titleSize="text-heading-300"
                  description={`You’re invited as a ${roleLabel(query.data.role)}. One Fleet login can belong to multiple accounts.`}
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
                            setError("Change your temporary password, then reopen this invitation.");
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
          </>
        )}
        {needsPasswordChange ? <Button variant="secondary" text="Change password" to="/update-password" /> : null}
        {error ? <Callout title={error} intent="danger" prefixIcon={<Alert />} /> : null}
        <Button variant="textOnly" text="Back to Fleet" to="/" />
      </main>
      <Footer />
    </div>
  );
};
export default AccountInvitation;
