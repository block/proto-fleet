import { afterEach, describe, expect, it, vi } from "vitest";
import { createAccountClient, createAccountConnection, hostedAccountsClient } from "./api";

const accountA = "11111111-1111-4111-8111-111111111111";
const accountB = "22222222-2222-4222-8222-222222222222";
afterEach(() => vi.unstubAllGlobals());

describe("hosted account request scope", () => {
  it("captures each page's account and leaves unscoped directory calls alone", async () => {
    const requests: { account: string | null; body: unknown }[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_input: Parameters<typeof fetch>[0], init: NonNullable<Parameters<typeof fetch>[1]>) => {
        requests.push({
          account: new Headers(init.headers).get("X-Fleet-Account-ID"),
          body: JSON.parse(
            typeof init.body === "string" ? init.body : new TextDecoder().decode(init.body as Uint8Array),
          ),
        });
        expect(init.credentials).toBe("include");
        return new Response("{}", { headers: { "Content-Type": "application/json" } });
      }),
    );
    const a = createAccountClient(accountA, new AbortController().signal);
    const b = createAccountClient(accountB, new AbortController().signal);
    await Promise.all([
      a.renameAccount({ accountId: accountA, name: "A" }),
      b.renameAccount({ accountId: accountB, name: "B" }),
    ]);
    await hostedAccountsClient.listAccounts({});
    expect(requests).toEqual([
      { account: accountA, body: { accountId: accountA, name: "A" } },
      { account: accountB, body: { accountId: accountB, name: "B" } },
      { account: null, body: {} },
    ]);
  });
  it("aborts in-flight work and rejects future calls after a page is disposed", async () => {
    let requestSignal: AbortSignal | undefined;
    vi.stubGlobal(
      "fetch",
      vi.fn(
        (_input: Parameters<typeof fetch>[0], init: NonNullable<Parameters<typeof fetch>[1]>) =>
          new Promise((_resolve, reject) => {
            requestSignal = init.signal as AbortSignal;
            requestSignal.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")), {
              once: true,
            });
          }),
      ),
    );
    const connection = createAccountConnection(accountA);
    const pending = connection.client.getAccount({ accountId: accountA });
    const rejected = expect(pending).rejects.toThrow();
    await vi.waitFor(() => expect(requestSignal).toBeDefined());
    connection.dispose();
    expect(requestSignal!.aborted).toBe(true);
    await rejected;
    await expect(connection.client.renameAccount({ accountId: accountA, name: "stale" })).rejects.toThrow();
  });
  it("reactivates a scope after effect replay without affecting another connection", async () => {
    const seen: string[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_input: Parameters<typeof fetch>[0], init: NonNullable<Parameters<typeof fetch>[1]>) => {
        expect(init.signal!.aborted).toBe(false);
        seen.push(new Headers(init.headers).get("X-Fleet-Account-ID")!);
        return new Response("{}", { headers: { "Content-Type": "application/json" } });
      }),
    );
    const old = createAccountConnection(accountA),
      current = createAccountConnection(accountB);
    old.dispose();
    current.dispose();
    current.activate();
    await current.client.getAccount({ accountId: accountB });
    await expect(old.client.getAccount({ accountId: accountA })).rejects.toThrow();
    expect(seen).toEqual([accountB]);
  });
});
