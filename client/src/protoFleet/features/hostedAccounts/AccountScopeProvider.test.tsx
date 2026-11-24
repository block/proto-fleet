import { StrictMode, useCallback, useLayoutEffect, useState } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { useRequiredAccountScope } from "./AccountScope";
import { AccountScopeProvider } from "./AccountScopeProvider";
import { useAccountMutation } from "./useAccountMutation";
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

it("runs mutations after StrictMode reactivation and aborts the current scope on unmount", async () => {
  const fetchMock = vi.fn(async () => new Response("{}", { headers: { "Content-Type": "application/json" } }));
  vi.stubGlobal("fetch", fetchMock);
  let signal!: () => AbortSignal;
  const Mutation = () => {
    const scope = useRequiredAccountScope();
    useLayoutEffect(() => {
      signal = () => scope.signal;
    }, [scope]);
    const mutation = useAccountMutation();
    const [saved, setSaved] = useState(false);
    return (
      <button
        onClick={async () => {
          if (
            await mutation.run(async () => {
              await scope.client.renameAccount({ accountId: "client", name: "Renamed" });
            })
          )
            setSaved(true);
        }}
      >
        {saved ? "Saved" : "Rename"}
      </button>
    );
  };
  const view = render(
    <StrictMode>
      <AccountScopeProvider
        account={create(AccountSchema, { accountId: "client" })}
        refreshAccount={vi.fn()}
        invalidateAccount={vi.fn()}
      >
        <Mutation />
      </AccountScopeProvider>
    </StrictMode>,
  );
  expect(signal().aborted).toBe(false);
  fireEvent.click(screen.getByRole("button", { name: "Rename" }));
  expect(await screen.findByRole("button", { name: "Saved" })).toBeInTheDocument();
  expect(fetchMock).toHaveBeenCalledTimes(1);
  view.unmount();
  expect(signal().aborted).toBe(true);
});
