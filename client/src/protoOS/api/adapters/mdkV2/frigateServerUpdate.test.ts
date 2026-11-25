import { describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { BinaryWriter, WireType } from "@bufbuild/protobuf/wire";
import { Code, ConnectError } from "@connectrpc/connect";
import { File } from "node:buffer";
import { createHash } from "node:crypto";
import {
  type FrigatePackage,
  FrigateUpdateError,
  type FrigateUpdateRecord,
  FrigateUpdateRefusedError,
} from "../frigateServerUpdate";
import { createFrigateServerUpdate, type ServerUpdateClient } from "./frigateServerUpdate";
import {
  GetServerUpdateSupportResponseSchema,
  ServerInstallationType,
  ServerUpdateRolloutPhase,
  ServerUpdateRolloutSchema,
} from "./generated/proto/mdk/v2/server_update_pb";
import { MdkV2Adapter } from "./MdkV2Adapter";

const MiB = 1024 * 1024;
const encoder = new TextEncoder();

const release = (size = 10, fields: Record<string, unknown> = {}) => {
  const deb = new Uint8Array(size).map((_, index) => index * 7);
  const statement = {
    schema: "frigate-release-statement/v1",
    package: "frigate",
    version: "9.9.9",
    architecture: "arm64",
    repository: "btc-mining/frigate",
    sha256: createHash("sha256").update(deb).digest("hex"),
    size,
    min_installed_version: "",
    issued_at: "2026-11-01T00:00:00Z",
    ...fields,
  };
  return {
    deb,
    files: [
      new File([deb], "frigate_9.9.9_linux_arm64.deb"),
      new File([encoder.encode(JSON.stringify(statement))], "frigate_9.9.9_linux_arm64.deb.statement.json"),
      new File([new Uint8Array(70)], "frigate_9.9.9_linux_arm64.deb.statement.sig"),
    ] as unknown as globalThis.File[],
  };
};

const support = (instanceId: string, peerInstanceId: string, fields = {}) =>
  create(GetServerUpdateSupportResponseSchema, {
    instanceId,
    peerInstanceId,
    runningVersion: "1.0.0",
    architecture: "arm64",
    installationType: ServerInstallationType.DEBIAN_PACKAGE,
    helperAvailable: true,
    clusterId: "cluster",
    spoolFreeBytes: BigInt(10 * MiB),
    etcdMembersTotal: 3,
    etcdMembersHealthy: 3,
    trustedReleaseKeyIds: ["sha256:key"],
    selfCoordinationSupported: true,
    remoteTriggerAllowed: true,
    ...fields,
  });

const rollout = (fields = {}) =>
  create(ServerUpdateRolloutSchema, {
    jobId: "job",
    targetVersion: "9.9.9",
    fromVersion: "1.0.0",
    phase: ServerUpdateRolloutPhase.VERIFYING,
    cancellable: true,
    ...fields,
  });

const refusal = (code: Code, reason: string, message = "refused") => {
  const error = new ConnectError(message, code);
  const value = new BinaryWriter()
    .tag(1, WireType.LengthDelimited)
    .string(reason)
    .tag(2, WireType.LengthDelimited)
    .string("mdk.v2.frigate")
    .finish();
  error.details.push({ type: "google.rpc.ErrorInfo", value });
  return error;
};

const headerOf = (options: unknown, name: string) =>
  (options as { headers?: Record<string, string> } | undefined)?.headers?.[name];

const setup = () => {
  const client = {
    getServerUpdateSupport: vi.fn(),
    beginServerUpdateStage: vi.fn(),
    appendServerUpdateStage: vi.fn(),
    commitServerUpdateStage: vi.fn(),
    abortServerUpdateStage: vi.fn(),
    startServerUpdate: vi.fn(),
    getServerUpdateStatus: vi.fn(),
    retryServerUpdate: vi.fn(),
    cancelServerUpdate: vi.fn(),
  };
  const update = createFrigateServerUpdate(client as unknown as ServerUpdateClient, (params = {}) => ({
    ...params,
    headers: { "X-Frigate-Actor": "proto-os", ...params.headers },
  }));
  return { client, update };
};

const record = (fields: Partial<FrigateUpdateRecord> = {}): FrigateUpdateRecord => ({
  jobId: "job",
  idempotencyKey: "key",
  targetVersion: "9.9.9",
  packageName: "frigate_9.9.9_linux_arm64.deb",
  instanceIds: ["ct1-a", "ct1-b"],
  fingerprints: { "ct1-a": "fa", "ct1-b": "fb" },
  step: "staged",
  ...fields,
});

describe("createFrigateServerUpdate", () => {
  describe("readPackage", () => {
    it("reads a release whose package matches its statement", async () => {
      const { update } = setup();
      const pkg = await update.readPackage(release().files);
      expect(pkg).toMatchObject({
        version: "9.9.9",
        architecture: "arm64",
        sizeBytes: 10,
        issuedAt: "2026-11-01T00:00:00Z",
      });
      expect(pkg.minInstalledVersion).toBeUndefined();
      expect(pkg.signature).toHaveLength(70);
    });

    it.each([
      ["an incomplete file set", () => release().files.slice(0, 2), "Choose the three files"],
      ["another package", () => release(10, { package: "other" }).files, 'statement is for "other"'],
      [
        "another repository",
        () => release(10, { repository: "fork/frigate" }).files,
        "not published from btc-mining/frigate",
      ],
      ["an unknown field", () => release(10, { extra: 1 }).files, 'unknown field "extra"'],
      ["a different size", () => release(10, { size: 11 }).files, "the file size differs"],
      ["a different digest", () => release(10, { sha256: "0".repeat(64) }).files, "the SHA-256 differs"],
      [
        "a package over the controllers' limit",
        () => release(10, { size: 512 * MiB + 1 }).files,
        "larger than the controllers accept (512 MiB)",
      ],
      [
        "an oversized statement",
        () => [
          release().files[0],
          new File([new Uint8Array(64 * 1024 + 1)], "x.deb.statement.json"),
          release().files[2],
        ],
        "not a controller software release statement",
      ],
      [
        "an oversized signature",
        () => [...release().files.slice(0, 2), new File([new Uint8Array(129)], "x.deb.statement.sig")],
        "not a controller software release signature",
      ],
    ])("rejects %s", async (_, files, message) => {
      const { update } = setup();
      await expect(update.readPackage(files() as globalThis.File[])).rejects.toThrow(message);
    });
  });

  it("hashes the package in pieces and stops when the request is aborted", async () => {
    const { update } = setup();
    const { files } = release(2 * MiB + 5);
    const slice = vi.spyOn(files[0], "slice");
    await update.readPackage(files);
    expect(slice.mock.calls).toEqual([
      [0, MiB],
      [MiB, 2 * MiB],
      [2 * MiB, 3 * MiB],
    ]);
    await expect(update.readPackage(files, { signal: AbortSignal.abort() })).rejects.toThrow();
  });

  describe("plan", () => {
    it("names the controller when only one already runs the release", async () => {
      const { client, update } = setup();
      const pkg = await update.readPackage(release().files);
      const versions = (a: string, b: string) =>
        client.getServerUpdateSupport.mockImplementation((_, options) =>
          headerOf(options, "x-frigate-server-id") === "ct1-b"
            ? support("ct1-b", "ct1-a", { runningVersion: b })
            : support("ct1-a", "ct1-b", { runningVersion: a }),
        );
      versions(pkg.version, "1.0.0");
      expect((await update.plan(pkg)).blockers).toEqual([
        expect.stringMatching(new RegExp(`^Controller ct1-a already runs version ${pkg.version}\\. .*by hand\\.$`)),
      ]);
      versions(pkg.version, pkg.version);
      expect((await update.plan(pkg)).blockers).toEqual([`The controllers already run version ${pkg.version}.`]);
    });

    it("refuses a release older than what a controller runs", async () => {
      const { client, update } = setup();
      const pkg = await update.readPackage(release().files);
      client.getServerUpdateSupport.mockImplementation((_, options) =>
        headerOf(options, "x-frigate-server-id") === "ct1-b"
          ? support("ct1-b", "ct1-a", { runningVersion: "10.0.0" })
          : support("ct1-a", "ct1-b"),
      );
      expect((await update.plan(pkg)).blockers).toEqual([
        "Controller ct1-b runs version 10.0.0, which is newer than this release. Only a newer release can be installed.",
      ]);
    });

    it("refuses a release that needs a newer installed version first", async () => {
      const { client, update } = setup();
      const pkg = { ...(await update.readPackage(release().files)), minInstalledVersion: "2.0.0" };
      client.getServerUpdateSupport.mockImplementation((_, options) =>
        headerOf(options, "x-frigate-server-id") === "ct1-b"
          ? support("ct1-b", "ct1-a", { runningVersion: "2.1.0" })
          : support("ct1-a", "ct1-b"),
      );
      expect((await update.plan(pkg)).blockers).toEqual([
        "This release needs version 2.0.0 or newer, and controller ct1-a runs 1.0.0. Install an earlier release first.",
      ]);
    });

    it.each([
      ["no self coordination", { selfCoordinationSupported: false }, 1],
      ["not a Debian install", { installationType: ServerInstallationType.UNSPECIFIED }, 1],
      ["no helper", { helperAvailable: false }, 1],
      ["exactly half of etcd healthy", { etcdMembersTotal: 2, etcdMembersHealthy: 1 }, 1],
      ["an etcd majority", { etcdMembersTotal: 3, etcdMembersHealthy: 2 }, 0],
      ["etcd membership not reported", { etcdMembersTotal: 0, etcdMembersHealthy: 0 }, 0],
      ["no trusted keys", { trustedReleaseKeyIds: [] }, 1],
      ["a different cluster", { clusterId: "other" }, 1],
      ["too little space", { spoolFreeBytes: 1n }, 1],
      ["space not reported", { spoolFreeBytes: 0n }, 0],
    ])("counts %s on one controller", async (_, fields, blockers) => {
      const { client, update } = setup();
      const pkg = await update.readPackage(release().files);
      client.getServerUpdateSupport.mockImplementation((_request, options) =>
        headerOf(options, "x-frigate-server-id") === "ct1-b"
          ? support("ct1-b", "ct1-a", fields)
          : support("ct1-a", "ct1-b"),
      );
      expect((await update.plan(pkg)).blockers).toHaveLength(blockers);
    });

    it("reads the peer through the local controller and checks the package against both", async () => {
      const { client, update } = setup();
      client.getServerUpdateSupport.mockImplementation((_, options) =>
        headerOf(options, "x-frigate-server-id") === "ct1-b"
          ? support("ct1-b", "ct1-a", { architecture: "amd64" })
          : support("ct1-a", "ct1-b", { remoteTriggerAllowed: false }),
      );
      const pkg = await update.readPackage(release().files);
      const plan = await update.plan(pkg);
      expect(plan.controllers.map((controller) => controller.instanceId)).toEqual(["ct1-a", "ct1-b"]);
      expect(plan.blockers).toEqual([
        expect.stringContaining("update.allow_remote_trigger"),
        "The package is built for arm64, but a controller runs on amd64.",
      ]);
      expect(headerOf(client.getServerUpdateSupport.mock.calls[0][1], "X-Frigate-Actor")).toBe("proto-os");
    });

    it("reports an unreachable peer and a running rollout", async () => {
      const { client, update } = setup();
      client.getServerUpdateSupport.mockImplementation((_, options) => {
        if (headerOf(options, "x-frigate-server-id"))
          throw new ConnectError("exact requested server is unavailable", Code.Unavailable);
        return support("ct1-a", "ct1-b", { activeJobId: "running" });
      });
      const plan = await update.plan(undefined);
      expect(plan.activeJobId).toBe("running");
      expect(plan.blockers).toEqual([
        "Controller ct1-b can't be reached: exact requested server is unavailable.",
        "Another software update is already running on these controllers.",
      ]);
    });
  });

  describe("stage", () => {
    it("copies the package to each controller in 1 MiB chunks and resumes from the controller's offset", async () => {
      const { client, update } = setup();
      const size = 2 * MiB + 5;
      const { files } = release(size);
      const pkg = (await update.readPackage(files)) as FrigatePackage;
      const staged = new Map<string, number>();
      client.beginServerUpdateStage.mockImplementation(({ manifest }) => ({
        manifestSha256: `m-${manifest.instanceId}`,
        nextOffset: BigInt(staged.get(manifest.instanceId) ?? 0),
      }));
      let failOnce = true;
      client.appendServerUpdateStage.mockImplementation((request, options) => {
        const id = headerOf(options, "x-frigate-server-id")!;
        // The first chunk to ct1-b is received but its answer is lost.
        staged.set(id, Number(request.offset) + request.chunk.length);
        if (id === "ct1-b" && failOnce) {
          failOnce = false;
          throw new ConnectError("connection reset", Code.Unavailable);
        }
        return { nextOffset: BigInt(staged.get(id)!) };
      });
      client.commitServerUpdateStage.mockImplementation(() => ({ sha256: pkg.sha256, bytesStaged: BigInt(size) }));
      const progress = vi.fn();

      const fingerprints = await update.stage(pkg, record({ fingerprints: {}, step: "staging" }), progress);

      expect(fingerprints).toEqual({ "ct1-a": "m-ct1-a", "ct1-b": "m-ct1-b" });
      const chunks = client.appendServerUpdateStage.mock.calls.map(([request, options]) => [
        headerOf(options, "x-frigate-server-id"),
        Number(request.offset),
        request.chunk.length,
      ]);
      expect(chunks).toEqual([
        ["ct1-a", 0, MiB],
        ["ct1-a", MiB, MiB],
        ["ct1-a", 2 * MiB, 5],
        ["ct1-b", 0, MiB],
        ["ct1-b", MiB, MiB],
        ["ct1-b", 2 * MiB, 5],
      ]);
      const manifest = client.beginServerUpdateStage.mock.calls[0][0].manifest;
      expect(manifest).toMatchObject({
        jobId: "job",
        instanceId: "ct1-a",
        expectedInstanceIds: ["ct1-a", "ct1-b"],
        coordination: 1,
        actor: "proto-os",
      });
      expect(manifest.maintenanceToken).toBeUndefined();
      expect(progress).toHaveBeenLastCalledWith(100, fingerprints);
    });

    it("rejects a controller copy that doesn't match the package", async () => {
      const { client, update } = setup();
      const pkg = await update.readPackage(release().files);
      client.beginServerUpdateStage.mockResolvedValue({ manifestSha256: "m", nextOffset: 10n });
      client.commitServerUpdateStage.mockResolvedValue({ sha256: "0".repeat(64), bytesStaged: 10n });
      await expect(update.stage(pkg, record({ fingerprints: {}, step: "staging" }), vi.fn())).rejects.toThrow(
        "Controller ct1-a received a package that doesn't match the file.",
      );
    });

    const staging = async (size = 10) => {
      const { client, update } = setup();
      const pkg = await update.readPackage(release(size).files);
      client.beginServerUpdateStage.mockResolvedValue({ manifestSha256: "m", nextOffset: 0n });
      return {
        client,
        pkg,
        copy: (fields = {}) => update.stage(pkg, record({ fingerprints: {}, step: "staging", ...fields }), vi.fn()),
      };
    };

    it("refuses to continue a saved copy when the controller holds a different package", async () => {
      const { client, copy } = await staging();
      await expect(copy({ fingerprints: { "ct1-a": "saved" } })).rejects.toThrow(
        "Controller ct1-a restarted the copy with a different package.",
      );
      expect(client.appendServerUpdateStage).not.toHaveBeenCalled();
    });

    it("gives up after three resumes of the same chunk", async () => {
      const { client, copy } = await staging();
      client.appendServerUpdateStage.mockRejectedValue(new ConnectError("connection reset", Code.Unavailable));
      await expect(copy()).rejects.toThrow("connection reset");
      expect(client.appendServerUpdateStage).toHaveBeenCalledTimes(4);
      expect(client.beginServerUpdateStage).toHaveBeenCalledTimes(4);
    });

    it("stops when a resume finds a different package on the controller", async () => {
      const { client, copy } = await staging();
      client.appendServerUpdateStage.mockRejectedValue(new ConnectError("connection reset", Code.Unavailable));
      client.beginServerUpdateStage
        .mockResolvedValueOnce({ manifestSha256: "m", nextOffset: 0n })
        .mockResolvedValueOnce({ manifestSha256: "other", nextOffset: 0n });
      await expect(copy()).rejects.toThrow("Controller ct1-a restarted the copy with a different package.");
      expect(client.appendServerUpdateStage).toHaveBeenCalledOnce();
    });

    it("resumes from the controller's offset when it acknowledges an unexpected one", async () => {
      const { client, copy } = await staging();
      client.appendServerUpdateStage.mockResolvedValue({ nextOffset: 3n });
      await expect(copy()).rejects.toThrow("Controller ct1-a reported an unexpected copy offset.");
      expect(client.commitServerUpdateStage).not.toHaveBeenCalled();
    });

    it("refuses a package file that changed size since it was read", async () => {
      const { client, pkg, copy } = await staging();
      Object.defineProperty(pkg.deb, "size", { value: 11 });
      await expect(copy()).rejects.toThrow("The package file changed. Choose the release again.");
      expect(client.beginServerUpdateStage).not.toHaveBeenCalled();
    });
  });

  describe("start", () => {
    it("sends the record's key and fingerprints without an instance selector", async () => {
      const { client, update } = setup();
      client.startServerUpdate.mockResolvedValue({ rollout: rollout({ releaseKeyId: "sha256:key" }) });
      const started = await update.start(record());
      expect(started).toMatchObject({ phase: "verifying", ended: false, releaseKeyId: "sha256:key" });
      const [request, options] = client.startServerUpdate.mock.calls[0];
      expect(request).toMatchObject({
        jobId: "job",
        idempotencyKey: "key",
        expectedInstanceIds: ["ct1-a", "ct1-b"],
        expectedManifestSha256: { "ct1-a": "fa", "ct1-b": "fb" },
      });
      expect(headerOf(options, "x-frigate-server-id")).toBeUndefined();
    });

    it("reports Frigate's refusal reason", async () => {
      const { client, update } = setup();
      client.startServerUpdate.mockRejectedValue(
        refusal(Code.FailedPrecondition, "UPDATE_FIRMWARE_ACTIVE", "a firmware update is running"),
      );
      const failure = await update.start(record()).catch((error: unknown) => error);
      expect(failure).toBeInstanceOf(FrigateUpdateRefusedError);
      expect(failure).toMatchObject({ code: "UPDATE_FIRMWARE_ACTIVE", message: "a firmware update is running" });
    });

    // Only these codes mean Frigate changed nothing. A reason never does: Frigate also attaches one when it can't
    // tell whether its write landed (a failed etcd commit is Unavailable with UPDATE_ETCD_UNAVAILABLE).
    it.each([Code.InvalidArgument, Code.FailedPrecondition, Code.Unimplemented])(
      "reads code %s as a refusal, with or without a reason",
      async (code) => {
        const { client, update } = setup();
        client.startServerUpdate.mockRejectedValueOnce(new ConnectError("refused", code));
        expect(await update.start(record()).catch((error: unknown) => error)).toBeInstanceOf(FrigateUpdateRefusedError);
        client.retryServerUpdate.mockRejectedValueOnce(refusal(code, "UPDATE_NOT_RETRYABLE"));
        expect(await update.retry("job", "key").catch((error: unknown) => error)).toBeInstanceOf(
          FrigateUpdateRefusedError,
        );
      },
    );

    it.each([
      Code.Unavailable,
      Code.Aborted,
      Code.Internal,
      Code.DeadlineExceeded,
      Code.ResourceExhausted,
      Code.NotFound,
      Code.PermissionDenied,
      Code.AlreadyExists,
      Code.OutOfRange,
      Code.Unknown,
    ])("leaves the outcome of code %s unknown, with or without a reason", async (code) => {
      const { client, update } = setup();
      client.startServerUpdate.mockRejectedValueOnce(new ConnectError("frigate is stopped", code));
      const bare = await update.start(record()).catch((error: unknown) => error);
      expect(bare).not.toBeInstanceOf(FrigateUpdateError);
      expect(bare).toMatchObject({ message: "frigate is stopped" });
      client.cancelServerUpdate.mockRejectedValueOnce(refusal(code, "UPDATE_ETCD_UNAVAILABLE", "etcd is down"));
      const reasoned = await update.cancel("job", "key").catch((error: unknown) => error);
      expect(reasoned).not.toBeInstanceOf(FrigateUpdateRefusedError);
      // The reason still words the failure.
      expect(reasoned).toMatchObject({ code: "UPDATE_ETCD_UNAVAILABLE", message: "etcd is down" });
    });
  });

  it("reads a missing rollout as no rollout, and reports maintenance", async () => {
    const { client, update } = setup();
    client.getServerUpdateStatus.mockRejectedValueOnce(refusal(Code.NotFound, "UPDATE_NOT_FOUND"));
    expect(await update.status("job")).toEqual({ servedByMaintenance: false });
    client.getServerUpdateStatus.mockResolvedValueOnce({
      rollout: rollout({ phase: ServerUpdateRolloutPhase.INSTALLING, cancellable: false }),
      servedByMaintenance: true,
    });
    expect(await update.status(undefined)).toMatchObject({
      servedByMaintenance: true,
      rollout: { phase: "installing", ended: false },
    });
    expect(client.getServerUpdateStatus.mock.calls[1][0]).toEqual({ jobId: "" });
  });

  describe("discard", () => {
    const sent = (client: ReturnType<typeof setup>["client"]) =>
      client.abortServerUpdateStage.mock.calls.map(([request, options]) => [
        request.manifestSha256,
        headerOf(options, "x-frigate-server-id"),
      ]);

    it("removes every staged copy, and accepts a controller that holds no stage of this update", async () => {
      const { client, update } = setup();
      client.abortServerUpdateStage
        .mockRejectedValueOnce(new ConnectError("stage identity does not match", Code.InvalidArgument))
        .mockResolvedValue({});
      await update.discard(record());
      expect(sent(client)).toEqual([
        ["fa", "ct1-a"],
        ["fb", "ct1-b"],
      ]);
    });

    it("reports a job that has started, so it is followed and not forgotten", async () => {
      const { client, update } = setup();
      client.abortServerUpdateStage
        .mockResolvedValueOnce({})
        .mockRejectedValueOnce(refusal(Code.FailedPrecondition, "UPDATE_ACTIVE_JOB", "cancel it instead"));
      await expect(update.discard(record())).rejects.toMatchObject({ code: "UPDATE_ACTIVE_JOB" });
    });

    // ct1-b accepted the start of the copy, but its answer never arrived, so no fingerprint was saved for it.
    const lostBegin = () =>
      record({
        fingerprints: { "ct1-a": "fa" },
        release: { sha256: "ab".repeat(32), sizeBytes: 10, statement: btoa("{}"), signature: btoa("s") },
      });

    it("asks a controller with no saved fingerprint for its copy, and removes it", async () => {
      const { client, update } = setup();
      client.beginServerUpdateStage.mockResolvedValue({ manifestSha256: "fb", nextOffset: 0n });
      client.abortServerUpdateStage.mockResolvedValue({});
      await update.discard(lostBegin());
      expect(client.beginServerUpdateStage).toHaveBeenCalledTimes(1);
      const [{ manifest }, options] = client.beginServerUpdateStage.mock.calls[0];
      expect(headerOf(options, "x-frigate-server-id")).toBe("ct1-b");
      expect(manifest).toMatchObject({ jobId: lostBegin().jobId, instanceId: "ct1-b", sizeBytes: 10n });
      expect(new TextDecoder().decode(manifest?.releaseStatement)).toBe("{}");
      expect(sent(client).sort()).toEqual([
        ["fa", "ct1-a"],
        ["fb", "ct1-b"],
      ]);
    });

    it("treats a controller that refuses to begin as holding no copy", async () => {
      const { client, update } = setup();
      client.beginServerUpdateStage.mockRejectedValue(refusal(Code.FailedPrecondition, "UPDATE_STAGE_LIMIT"));
      client.abortServerUpdateStage.mockResolvedValue({});
      await update.discard(lostBegin());
      expect(sent(client)).toEqual([["fa", "ct1-a"]]);
    });

    it("keeps the update when a controller with no saved fingerprint doesn't answer", async () => {
      const { client, update } = setup();
      client.beginServerUpdateStage.mockRejectedValue(new ConnectError("connection reset", Code.Unavailable));
      client.abortServerUpdateStage.mockResolvedValue({});
      await expect(update.discard(lostBegin())).rejects.toThrow("Controller ct1-b didn't confirm");
    });

    it("rejects when a controller doesn't confirm, naming it", async () => {
      const { client, update } = setup();
      client.abortServerUpdateStage
        .mockResolvedValueOnce({})
        .mockRejectedValueOnce(new ConnectError("cannot read the rollout", Code.Unavailable));
      await expect(update.discard(record())).rejects.toThrow(
        "Controller ct1-b didn't confirm the package was removed (cannot read the rollout). Discard it again.",
      );
      expect(sent(client)).toHaveLength(2);
    });
  });
});

describe("MdkV2Adapter", () => {
  it("offers local software updates only over Connect", () => {
    expect(MdkV2Adapter.fromConfig({ baseUrl: "http://ct1", protocol: "connect" }).frigateServerUpdate).toBeDefined();
    expect(MdkV2Adapter.fromConfig({ baseUrl: "http://ct1", protocol: "rest" }).frigateServerUpdate).toBeUndefined();
  });
});
