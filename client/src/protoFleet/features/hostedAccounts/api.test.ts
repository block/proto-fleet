import { afterEach, describe, expect, it, vi } from "vitest";
import { createClient } from "@connectrpc/connect";
import { createAccountClient, createAccountConnection, createAccountTransport, hostedAccountsClient } from "./api";
import { PoolConfigurationService } from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";

const accountA = "11111111-1111-4111-8111-111111111111";
const accountB = "22222222-2222-4222-8222-222222222222";
afterEach(() => vi.unstubAllGlobals());

describe("hosted account request scope", () => {
  it("permits preview reads but blocks mutations before sending them", async () => {
    const fetcher = vi.fn(async () => new Response("{}", { headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetcher);
    const preview = createAccountConnection(accountA, { readOnly: true });
    await preview.client.getAccount({ accountId: accountA });
    await expect(preview.client.renameAccount({ accountId: accountA, name: "Changed" })).rejects.toThrow(/preview/i);
    expect(fetcher).toHaveBeenCalledTimes(1);
  });
  it("allows saved-analysis preview reads and exports but rejects generation in the real interceptor", async () => {
    const fetcher = vi.fn(async (input: Parameters<typeof fetch>[0]) =>
      String(input).endsWith("/ExportAccountAnalysis")
        ? new Response(new Uint8Array([2, 0, 0, 0, 2, 123, 125]), {
            headers: { "Content-Type": "application/connect+json" },
          })
        : new Response("{}", { headers: { "Content-Type": "application/json" } }),
    );
    vi.stubGlobal("fetch", fetcher);
    const preview = createAccountConnection(accountA, { readOnly: true });
    await preview.client.getAccountAnalysis({ accountId: accountA, analysisId: "saved" });
    const chunks = [];
    for await (const chunk of preview.client.exportAccountAnalysis({ accountId: accountA, analysisId: "saved" }))
      chunks.push(chunk);
    expect(chunks).toEqual([]);
    await expect(preview.client.generateAccountAnalysis({ accountId: accountA })).rejects.toThrow(/preview/i);
    expect(fetcher).toHaveBeenCalledTimes(2);
  });
  it("allows canonical configuration checks in preview while refusing review confirmation", async () => {
    const fetcher = vi.fn(async () => new Response("{}", { headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetcher);
    const client = createClient(
      PoolConfigurationService,
      createAccountTransport(accountA, new AbortController().signal, { readOnly: true }),
    );
    await client.checkConfiguration({ accountId: accountA, target: { kind: "device", targetId: 42n } });
    await client.readDevicePools({ accountId: accountA, targets: [{ kind: "device", targetId: 42n }] });
    await expect(client.confirmReview({ accountId: accountA, reviewId: "review", password: "secret" })).rejects.toThrow(
      /preview/i,
    );
    expect(fetcher).toHaveBeenCalledTimes(2);
  });
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
