import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, test, vi } from "vitest";

import { MinerHostingProvider } from "./MinerHostingContext";
import { useMinerHosting } from "./useMinerHosting";

const jsonResponse = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

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
    vi.stubGlobal("fetch", vi.fn<typeof fetch>().mockResolvedValue(jsonResponse({ unexpected: true })));

    render(
      <MinerHostingProvider>
        <div>Miner application</div>
      </MinerHostingProvider>,
    );

    await screen.findByRole("heading", { name: "This miner isn't supported yet" });
    expect(screen.getByText("ProtoOS couldn't identify a supported miner API.")).toBeInTheDocument();
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
