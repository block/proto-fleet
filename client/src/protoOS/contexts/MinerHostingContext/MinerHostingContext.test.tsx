import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, test, vi } from "vitest";

import { MinerHostingProvider } from "./MinerHostingContext";
import { useMinerHosting } from "./useMinerHosting";

const jsonResponse = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

const htmlResponse = (status = 200) =>
  new Response("<!doctype html><html><body>captive portal</body></html>", {
    status,
    headers: { "Content-Type": "text/html" },
  });

const ContextState = () => {
  const { resolution } = useMinerHosting();
  return <div data-testid="resolution">{`${resolution.status}:${resolution.adapterId ?? "none"}`}</div>;
};

describe("MinerHostingProvider adapter resolution", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  test("probes MDK v2 before MDK v1 and exposes the resolved v1 adapter", async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(jsonResponse({}, 404))
      .mockResolvedValueOnce(jsonResponse({ "system-info": { product_name: "Proto Rig" } }));
    vi.stubGlobal("fetch", fetchMock);

    render(
      <MinerHostingProvider>
        <ContextState />
      </MinerHostingProvider>,
    );

    expect(screen.getByTestId("resolution")).toHaveTextContent("probing:none");
    await waitFor(() => expect(screen.getByTestId("resolution")).toHaveTextContent("ready:mdk-v1"));
    expect(fetchMock.mock.calls.map(([input]) => String(input))).toEqual(["/api/v2/system/info", "/api/v1/system"]);
  });

  test("exposes a ready adapter when a CT1 MDK v2 response is detected", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>().mockResolvedValue(
        jsonResponse({
          info: { id: "container-1", version: "v2 (generated from frigate e87a5197)", product: "CT1" },
        }),
      ),
    );

    render(
      <MinerHostingProvider>
        <ContextState />
      </MinerHostingProvider>,
    );

    await waitFor(() => expect(screen.getByTestId("resolution")).toHaveTextContent("ready:mdk-v2"));
  });

  test("keeps an MDK v2 R2 on the unsupported page until its adapter is implemented", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>().mockResolvedValue(
        jsonResponse({
          info: { id: "rig-1", version: "v2.0.0", product: "R2" },
        }),
      ),
    );

    render(
      <MinerHostingProvider>
        <div>Miner application</div>
      </MinerHostingProvider>,
    );

    await screen.findByRole("heading", { name: "This miner isn't supported yet" });
    expect(screen.getByText("MDK v2 was detected, but ProtoOS support is still being built.")).toBeInTheDocument();
  });

  test("keeps an unrecognized MDK v2 product on the unsupported page", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>().mockResolvedValue(
        jsonResponse({
          info: { id: "unknown-1", version: "v2.0.0", product: "Frigate" },
        }),
      ),
    );

    render(
      <MinerHostingProvider>
        <div>Miner application</div>
      </MinerHostingProvider>,
    );

    await screen.findByRole("heading", { name: "This miner isn't supported yet" });
    expect(screen.getByText("MDK v2 was detected, but ProtoOS support is still being built.")).toBeInTheDocument();
  });

  test("shows a generic unsupported page when no registered probe matches", async () => {
    // A miner that answers every probe path with the same generic body. Each
    // probe is a separate fetch, so hand back a fresh Response per call (a real
    // fetch never yields the same body-consumed Response twice).
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>().mockImplementation(() => Promise.resolve(jsonResponse({ unexpected: true }))),
    );

    render(
      <MinerHostingProvider>
        <div>Miner application</div>
      </MinerHostingProvider>,
    );

    await screen.findByRole("heading", { name: "This miner isn't supported yet" });
    expect(screen.getByText("ProtoOS couldn't identify a supported miner API.")).toBeInTheDocument();
  });

  test("treats a 2xx non-JSON body as unsupported, not a retryable unreachable", async () => {
    // A captive portal / reverse proxy answering 200 with HTML on every probe
    // path. A non-JSON 2xx body is a well-formed non-match, not a probe error,
    // so resolution must be terminal "unsupported" — never the retry loop.
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>().mockImplementation(() => Promise.resolve(htmlResponse())),
    );

    render(
      <MinerHostingProvider>
        <div>Miner application</div>
      </MinerHostingProvider>,
    );

    await screen.findByRole("heading", { name: "This miner isn't supported yet" });
  });

  test("treats an empty 2xx body as unsupported, not a retryable unreachable", async () => {
    // A 200 with an empty body fails JSON parsing just like a non-JSON body; it
    // must resolve to a well-formed non-match (terminal "unsupported"), not a
    // probe error (retryable "unreachable").
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>().mockImplementation(() => Promise.resolve(new Response("", { status: 200 }))),
    );

    render(
      <MinerHostingProvider>
        <div>Miner application</div>
      </MinerHostingProvider>,
    );

    await screen.findByRole("heading", { name: "This miner isn't supported yet" });
  });

  test("never exposes the raw miner client on window.api", async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(jsonResponse({}, 404))
      .mockResolvedValueOnce(jsonResponse({ "system-info": { product_name: "Proto Rig" } }));
    vi.stubGlobal("fetch", fetchMock);

    render(
      <MinerHostingProvider>
        <ContextState />
      </MinerHostingProvider>,
    );

    await waitFor(() => expect(screen.getByTestId("resolution")).toHaveTextContent("ready:mdk-v1"));
    // The provider must resolve an adapter without ever leaking the raw generated
    // client onto the global (the #59 invariant the removed window.api violated).
    expect((window as unknown as { api?: unknown }).api).toBeUndefined();
  });

  test("returns to probing when only the hosting mode changes", async () => {
    let call = 0;
    const fetchMock = vi.fn<typeof fetch>((input, init) => {
      call += 1;
      const url = String(input);
      // Direct pass (calls 1-2): v2 misses, v1 identifies the miner.
      if (call <= 2) {
        return Promise.resolve(
          url.endsWith("/api/v2/system/info")
            ? jsonResponse({}, 404)
            : jsonResponse({ "system-info": { product_name: "Proto Rig" } }),
        );
      }
      // Fleet re-probe: never settle (except on abort) so the new probe stays pending.
      return new Promise<Response>((_resolve, reject) => {
        init?.signal?.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")));
      });
    });
    vi.stubGlobal("fetch", fetchMock);

    const { rerender } = render(
      <MinerHostingProvider baseUrl="/miner-one" mode="direct">
        <ContextState />
      </MinerHostingProvider>,
    );
    await waitFor(() => expect(screen.getByTestId("resolution")).toHaveTextContent("ready:mdk-v1"));

    rerender(
      <MinerHostingProvider baseUrl="/miner-one" mode="fleet">
        <ContextState />
      </MinerHostingProvider>,
    );

    // Same baseUrl but a different mode selects a different credential, so the
    // prior mode's resolved adapter must not flash through during the switch.
    expect(screen.getByTestId("resolution")).toHaveTextContent("probing:none");
  });

  test("aborts an in-flight probe and ignores its result when the miner changes", async () => {
    let firstProbeSignal: AbortSignal | undefined;
    const fetchMock = vi.fn<typeof fetch>((input, init) => {
      const url = String(input);
      if (url === "/miner-one/api/v2/system/info") {
        firstProbeSignal = init?.signal ?? undefined;
        return new Promise<Response>((_resolve, reject) => {
          firstProbeSignal?.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")));
        });
      }
      if (url === "/miner-two/api/v2/system/info") {
        return Promise.resolve(jsonResponse({ info: { version: "v2", product: "CT1" } }));
      }
      return Promise.resolve(jsonResponse({}, 404));
    });
    vi.stubGlobal("fetch", fetchMock);

    const { rerender } = render(
      <MinerHostingProvider baseUrl="/miner-one">
        <ContextState />
      </MinerHostingProvider>,
    );
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/miner-one/api/v2/system/info", expect.anything()));

    rerender(
      <MinerHostingProvider baseUrl="/miner-two">
        <ContextState />
      </MinerHostingProvider>,
    );

    await waitFor(() => expect(screen.getByTestId("resolution")).toHaveTextContent("ready:mdk-v2"));
    expect(firstProbeSignal?.aborted).toBe(true);
  });

  test("renders a retryable state when the miner is unreachable and re-probes on retry", async () => {
    let attempt = 0;
    const fetchMock = vi.fn<typeof fetch>((input) => {
      const url = String(input);
      attempt += 1;
      // First probe pass (v2 then v1) fails with a non-abort network error.
      if (attempt <= 2) return Promise.reject(new TypeError("Failed to fetch"));
      // Retry pass: v2 misses, v1 identifies the miner.
      if (url.endsWith("/api/v2/system/info")) return Promise.resolve(jsonResponse({}, 404));
      return Promise.resolve(jsonResponse({ "system-info": { product_name: "Proto Rig" } }));
    });
    vi.stubGlobal("fetch", fetchMock);

    render(
      <MinerHostingProvider>
        <ContextState />
      </MinerHostingProvider>,
    );

    await screen.findByRole("heading", { name: "Couldn't reach this miner" });
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));

    await waitFor(() => expect(screen.getByTestId("resolution")).toHaveTextContent("ready:mdk-v1"));
  });

  test("resolves to the retryable unreachable state when a probe never settles (per-attempt timeout)", async () => {
    vi.useFakeTimers();
    // A probe that never resolves on its own, only rejecting when its request
    // signal is aborted (mirrors fetch's abort behavior). The per-attempt
    // timeout must abort it so resolution doesn't hang forever.
    const fetchMock = vi.fn<typeof fetch>(
      (_input, init) =>
        new Promise<Response>((_resolve, reject) => {
          init?.signal?.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")));
        }),
    );
    vi.stubGlobal("fetch", fetchMock);

    render(
      <MinerHostingProvider>
        <ContextState />
      </MinerHostingProvider>,
    );

    // Advance past both probes' timeouts (they run sequentially).
    await act(async () => {
      await vi.advanceTimersByTimeAsync(11000);
    });

    expect(screen.getByRole("heading", { name: "Couldn't reach this miner" })).toBeInTheDocument();
  });

  test("returns to probing immediately after a baseUrl change, before the new probe settles", async () => {
    const fetchMock = vi.fn<typeof fetch>((input, init) => {
      const url = String(input);
      if (url.startsWith("/miner-one")) {
        return Promise.resolve(
          url.endsWith("/api/v2/system/info")
            ? jsonResponse({}, 404)
            : jsonResponse({ "system-info": { product_name: "Proto Rig" } }),
        );
      }
      // miner-two: never settle (except on abort) so the new probe stays pending.
      return new Promise<Response>((_resolve, reject) => {
        init?.signal?.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")));
      });
    });
    vi.stubGlobal("fetch", fetchMock);

    const { rerender } = render(
      <MinerHostingProvider baseUrl="/miner-one">
        <ContextState />
      </MinerHostingProvider>,
    );
    await waitFor(() => expect(screen.getByTestId("resolution")).toHaveTextContent("ready:mdk-v1"));

    rerender(
      <MinerHostingProvider baseUrl="/miner-two">
        <ContextState />
      </MinerHostingProvider>,
    );

    // The prior miner's resolved adapter must not flash through during the
    // switch: the new instance has no settled resolution yet, so we show probing.
    expect(screen.getByTestId("resolution")).toHaveTextContent("probing:none");
  });
});
