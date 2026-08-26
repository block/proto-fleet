import { render, screen, waitFor } from "@testing-library/react";
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

  test("shows the temporary unsupported page when MDK v2 is detected", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>().mockResolvedValue(
        jsonResponse({
          info: { id: "container-1", version: "v2 (generated from frigate e87a5197)", product: "CT1" },
        }),
      ),
    );

    render(
      <MinerHostingProvider closeButton={<button type="button">Back to fleet</button>}>
        <div>Miner application</div>
      </MinerHostingProvider>,
    );

    expect(screen.getByText("Miner application")).toBeInTheDocument();
    await screen.findByRole("heading", { name: "This miner isn't supported yet" });
    expect(screen.getByText("MDK v2 was detected, but ProtoOS support is still being built.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Back to fleet" })).toBeInTheDocument();
    expect(screen.queryByText("Miner application")).not.toBeInTheDocument();
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

    await screen.findByRole("heading", { name: "This miner isn't supported yet" });
    expect(screen.getByText("MDK v2 was detected, but ProtoOS support is still being built.")).toBeInTheDocument();
    expect(firstProbeSignal?.aborted).toBe(true);
  });
});
