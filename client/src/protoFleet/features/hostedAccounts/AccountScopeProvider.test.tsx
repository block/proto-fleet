import { StrictMode, useCallback } from "react";
import { render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { useRequiredAccountScope } from "./AccountScope";
import { AccountScopeProvider } from "./AccountScopeProvider";
import { useAccountQuery } from "./useAccountQuery";
import { AccountSchema } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";

afterEach(() => vi.unstubAllGlobals());
const Query = () => {
  const { client, account } = useRequiredAccountScope();
  const load = useCallback(
    (signal: AbortSignal) => client.getAccount({ accountId: account.accountId }, { signal }),
    [client, account.accountId],
  );
  const query = useAccountQuery(load);
  return <p>{query.data?.account?.name ?? query.error ?? "Loading"}</p>;
};
it("reactivates the scope before child reads during StrictMode effect replay", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response(JSON.stringify({ account: { accountId: "client", name: "Scoped account" } }), {
          headers: { "Content-Type": "application/json" },
        }),
    ),
  );
  render(
    <StrictMode>
      <AccountScopeProvider
        account={create(AccountSchema, { accountId: "client" })}
        refreshAccount={vi.fn()}
        invalidateAccount={vi.fn()}
      >
        <Query />
      </AccountScopeProvider>
    </StrictMode>,
  );
  expect(await screen.findByText("Scoped account")).toBeInTheDocument();
});
