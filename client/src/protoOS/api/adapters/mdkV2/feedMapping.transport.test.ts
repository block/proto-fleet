import { describe, expect, it } from "vitest";
import { Code, ConnectError, createRouterTransport } from "@connectrpc/connect";
import { createMdkV2Client } from "./connectClient";
import { FeedMappingRunStatus, FeedMappingService } from "./generated/proto/mdk/v2/feed_mapping_pb";
import { HashboardService } from "./generated/proto/mdk/v2/hashboard_pb";
import { MdkV2Adapter } from "./MdkV2Adapter";

describe("CT1 mapping transport", () => {
  it("plans without mutation, confirms the exact feed scope, and decorates requests", async () => {
    const seen: unknown[] = [];
    const transport = createRouterTransport((router) => {
      router.rpc(FeedMappingService.method.planFeedMappingRun, (request, context) => {
        seen.push(
          request.config?.dryRun,
          request.config?.confirmPowerCycle,
          context.requestHeader.get("X-Frigate-Actor"),
        );
        return {
          plan: { runId: "plan", status: FeedMappingRunStatus.PLANNED, feeds: [{ feedId: "pdu-1-outlet-01" }] },
        };
      });
      router.rpc(FeedMappingService.method.startFeedMappingRun, (request) => {
        seen.push(
          request.config?.feedIds,
          request.config?.confirmPowerCycle,
          request.config?.restoreAllOn,
          request.config?.dryRun,
        );
        return { run: { runId: "run-1", status: FeedMappingRunStatus.RUNNING } };
      });
    });
    const adapter = new MdkV2Adapter(createMdkV2Client(transport));
    const plan = await adapter.feedMapping!.plan({ headers: { "X-Frigate-Actor": "test-operator" } });
    const run = await adapter.feedMapping!.start(plan.feeds.map((f) => f.id));
    expect(seen).toEqual([true, false, "test-operator", ["pdu-1-outlet-01"], true, true, false]);
    expect(run).toMatchObject({ id: "run-1", status: "running" });
  });

  it("treats only NotFound as no active run", async () => {
    let code = Code.NotFound;
    const adapter = new MdkV2Adapter(
      createMdkV2Client(
        createRouterTransport((router) => {
          router.rpc(FeedMappingService.method.getActiveFeedMappingRun, () => {
            throw new ConnectError("no run", code);
          });
        }),
      ),
    );
    expect(await adapter.feedMapping!.active()).toBeNull();
    code = Code.Unavailable;
    await expect(adapter.feedMapping!.active()).rejects.toThrow();
  });

  it("distinguishes refused starts from unknown outcomes", async () => {
    let code = Code.FailedPrecondition;
    const adapter = new MdkV2Adapter(
      createMdkV2Client(
        createRouterTransport((router) => {
          router.rpc(FeedMappingService.method.startFeedMappingRun, () => {
            throw new ConnectError("not primary", code);
          });
        }),
      ),
    );
    await expect(adapter.feedMapping!.start(["feed"])).rejects.toMatchObject({ writeOutcome: "refused" });
    code = Code.Unavailable;
    await expect(adapter.feedMapping!.start(["feed"])).rejects.toMatchObject({ writeOutcome: "unknown" });
  });

  it("treats only NotFound as a missing saved run", async () => {
    let code = Code.NotFound;
    const adapter = new MdkV2Adapter(
      createMdkV2Client(
        createRouterTransport((router) => {
          router.rpc(FeedMappingService.method.getFeedMappingRun, () => {
            throw new ConnectError("run unavailable", code);
          });
        }),
      ),
    );
    expect(await adapter.feedMapping!.get("lost-run")).toBeNull();
    code = Code.Unavailable;
    await expect(adapter.feedMapping!.get("lost-run")).rejects.toThrow();
  });

  it("preserves 0/0 inventory through the binary Connect response", async () => {
    const adapter = new MdkV2Adapter(
      createMdkV2Client(
        createRouterTransport((router) => {
          router.rpc(HashboardService.method.getContainerMap, () => ({
            containerMap: {
              tanks: [{ modules: [{ slots: [{ occupied: true, hashboard: { macAddress: "board" } }] }] }],
            },
          }));
          router.rpc(FeedMappingService.method.getFeedMappingReport, () => ({ wiring: { mode: "observe" } }));
        }),
      ),
    );
    expect((await adapter.feedMapping!.assess()).boards[0]).toMatchObject({ tank: 0, module: 0, unlocated: true });
  });
});
