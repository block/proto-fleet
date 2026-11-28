// @vitest-environment node
import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import {
  describeFrigateError,
  frigateAgentPhaseLabel,
  frigateCodeText,
  frigateRolloutPhaseLabel,
  frigateStateLabel,
  instanceSupportProblems,
  packageKindFromName,
  packageKindRules,
  protoOSJobStateLabel,
  UPDATABLE_KIND_RULES,
} from "./frigateUpdate";
import {
  FrigateAgentPhase,
  FrigateInstallationType,
  FrigatePackageKind,
  FrigateRolloutPhase,
  FrigateUpdateState,
  InstanceSupportSchema,
  ProtoOSBundleSource,
  ProtoOSJobState,
} from "@/protoFleet/api/generated/frigateupdate/v1/frigateupdate_pb";

describe("describeFrigateError", () => {
  it("maps a known code to readable text and keeps the server message as detail", () => {
    expect(describeFrigateError("UPDATE_INSTALL_FAILED", "dpkg exited 1")).toEqual({
      title: "Installing the package failed on a controller.",
      detail: "dpkg exited 1",
      code: "UPDATE_INSTALL_FAILED",
    });
  });

  it("maps the same condition from either prefix", () => {
    expect(frigateCodeText("FLEET_REMOTE_TRIGGER_DISABLED")).toBe(frigateCodeText("UPDATE_REMOTE_TRIGGER_DISABLED"));
    expect(frigateCodeText("FLEET_REMOTE_TRIGGER_DISABLED")).toMatch(/allow_remote_trigger/);
  });

  it("explains a server update refused because firmware is being flashed", () => {
    expect(describeFrigateError("UPDATE_FIRMWARE_ACTIVE", "frigate-a hashboard job-1")).toEqual({
      title:
        "A hashboard or PSU firmware update is running on a controller. Wait for it to finish, then start the update again.",
      detail: "frigate-a hashboard job-1",
      code: "UPDATE_FIRMWARE_ACTIVE",
    });
  });

  it.each(["UPDATE_COOLDOWN", "UPDATE_OUTSIDE_WINDOW", "UPDATE_NOT_STAGED", "UPDATE_STAGE_MISMATCH", "UPDATE_EXPIRED"])(
    "maps the Frigate refusal %s",
    (code) => {
      expect(frigateCodeText(code)).toBeDefined();
    },
  );

  it("falls back to the server message for an unknown code and still reports the code", () => {
    expect(describeFrigateError("UPDATE_SOMETHING_NEW", "the disk caught fire")).toEqual({
      title: "the disk caught fire",
      detail: undefined,
      code: "UPDATE_SOMETHING_NEW",
    });
  });

  it("falls back to a generic message when an unknown code has no message", () => {
    expect(describeFrigateError("FLEET_MYSTERY", "")).toEqual({
      title: "The update failed.",
      detail: undefined,
      code: "FLEET_MYSTERY",
    });
  });
});

describe("labels", () => {
  it("labels states and phases, with a neutral fallback for unreported ones", () => {
    expect(frigateStateLabel(FrigateUpdateState.RUNNING)).toBe("Update in progress");
    expect(frigateStateLabel(FrigateUpdateState.UNSPECIFIED)).toBe("Unknown");
    expect(frigateRolloutPhaseLabel(FrigateRolloutPhase.INSTALLING)).toBe("Installing");
    expect(frigateRolloutPhaseLabel(FrigateRolloutPhase.UNSPECIFIED)).toBe("");
    expect(frigateAgentPhaseLabel(FrigateAgentPhase.STOPPED)).toBe("Software stopped");
    expect(frigateAgentPhaseLabel(FrigateAgentPhase.UNSPECIFIED)).toBe("Not reported");
  });
});

describe("instanceSupportProblems", () => {
  const ready = {
    instanceId: "a",
    remoteTriggerAllowed: true,
    helperAvailable: true,
    selfCoordinationSupported: true,
    installationType: FrigateInstallationType.DEBIAN_PACKAGE,
    etcdMembersTotal: 3,
    etcdMembersHealthy: 3,
  };

  it("reports nothing for a ready controller", () => {
    expect(instanceSupportProblems(create(InstanceSupportSchema, ready))).toEqual([]);
  });

  it("explains a disabled remote trigger as an action on the controller", () => {
    const [problem] = instanceSupportProblems(create(InstanceSupportSchema, { ...ready, remoteTriggerAllowed: false }));
    expect(problem).toMatch(/update\.allow_remote_trigger/);
  });

  it("lists every problem, including etcd health and a running update", () => {
    const problems = instanceSupportProblems(
      create(InstanceSupportSchema, {
        ...ready,
        helperAvailable: false,
        selfCoordinationSupported: false,
        installationType: FrigateInstallationType.DIRECT_BINARY,
        etcdMembersHealthy: 1,
        activeJobId: "job",
      }),
    );
    expect(problems).toHaveLength(5);
    expect(problems.join(" ")).toMatch(/1 of 3 members are healthy/);
  });

  it("flags an unreported installation type, which the server also refuses", () => {
    const problems = instanceSupportProblems(
      create(InstanceSupportSchema, { ...ready, installationType: FrigateInstallationType.UNSPECIFIED }),
    );
    expect(problems).toEqual([
      "A controller wasn't installed from a Debian package, so it can't be updated from Fleet.",
    ]);
  });

  it("reports etcd only once quorum is lost, as the server does", () => {
    expect(instanceSupportProblems(create(InstanceSupportSchema, { ...ready, etcdMembersHealthy: 2 }))).toEqual([]);
    const lost = instanceSupportProblems(
      create(InstanceSupportSchema, { ...ready, etcdMembersTotal: 2, etcdMembersHealthy: 1 }),
    );
    expect(lost.join(" ")).toMatch(/Restore quorum.*1 of 2 members are healthy/);
  });

  it("reports only the read failure when the controller could not be read", () => {
    expect(instanceSupportProblems(create(InstanceSupportSchema, { instanceId: "a", error: "timed out" }))).toEqual([
      "Fleet couldn't read this controller: timed out",
    ]);
  });
});

describe("package kinds", () => {
  const controllerSoftware = UPDATABLE_KIND_RULES[FrigatePackageKind.CONTROLLER_SOFTWARE];
  const protoOS = UPDATABLE_KIND_RULES[FrigatePackageKind.PROTO_OS];

  it("names each kind as the Fleet UI does", () => {
    expect([controllerSoftware.title, controllerSoftware.noun]).toEqual(["Controller software", "controller software"]);
    expect([protoOS.title, protoOS.noun]).toEqual(["ProtoOS", "ProtoOS"]);
  });

  it("offers Retry only for controller software, which the server can resume", () => {
    expect(controllerSoftware.retryConfirm).toMatch(/resumes from the step that failed/);
    expect(protoOS.retryConfirm).toBeUndefined();
  });

  it("warns that a ProtoOS install keeps the controllers running and can't be cancelled after a commit", () => {
    expect(protoOS.startWarning.title).toBe("An install can't be cancelled after a controller commits its copy.");
    expect(protoOS.startWarning.subtitle).toBe("The controllers and mining keep running during the update.");
    expect(controllerSoftware.startWarning.title).toBe("Both controllers stop mining control together, briefly.");
  });

  it.each([FrigatePackageKind.UNSPECIFIED, 42 as FrigatePackageKind])(
    "treats the unknown kind %s as other software with no Retry",
    (kind) => {
      const rules = packageKindRules(kind);
      expect(rules.title).toBe("Other software");
      expect(rules.retryConfirm).toBeUndefined();
    },
  );

  it("reads the kind names the server stores", () => {
    expect(packageKindFromName("frigate")).toBe(FrigatePackageKind.CONTROLLER_SOFTWARE);
    expect(packageKindFromName("proto_os")).toBe(FrigatePackageKind.PROTO_OS);
    for (const name of ["", undefined, "firmware", "toString"]) {
      expect(packageKindFromName(name)).toBe(FrigatePackageKind.UNSPECIFIED);
    }
  });

  it("maps the ProtoOS refusal codes and keeps unknown ones generic", () => {
    expect(frigateCodeText("FLEET_PROTOOS_UNSUPPORTED")).toMatch(/Update its controller software first/);
    expect(frigateCodeText("FLEET_PROTOOS_REPOSITORY_UNTRUSTED")).toMatch(/repository/);
    expect(describeFrigateError("FLEET_PROTOOS_INSTALL_FAILED", "dpkg exited 1")).toEqual({
      title: "A controller couldn't install ProtoOS.",
      detail: "dpkg exited 1",
      code: "FLEET_PROTOOS_INSTALL_FAILED",
    });
    expect(frigateCodeText("FLEET_PROTOOS_JOB_NOT_REPORTED")).toBeDefined();
    expect(frigateCodeText("FLEET_PROTOOS_INSTALL_TIMEOUT")).toBeDefined();
  });

  it("labels ProtoOS job states, with a neutral label for one this client doesn't know", () => {
    expect(protoOSJobStateLabel(ProtoOSJobState.PROTO_OS_JOB_STATE_INSTALLING)).toBe("Installing");
    expect(protoOSJobStateLabel(ProtoOSJobState.PROTO_OS_JOB_STATE_UNSPECIFIED)).toBe("");
    expect(protoOSJobStateLabel(ProtoOSJobState.PROTO_OS_JOB_STATE_UNKNOWN)).toBe("Unknown state");
    expect(protoOSJobStateLabel(42 as ProtoOSJobState)).toBe("Unknown state");
  });
});

describe("ProtoOS controller support", () => {
  const support = UPDATABLE_KIND_RULES[FrigatePackageKind.PROTO_OS].support;
  const ready = (overrides: Record<string, unknown> = {}) =>
    create(InstanceSupportSchema, {
      instanceId: "a",
      remoteTriggerAllowed: true,
      protoOs: {
        selfSupported: true,
        servedSource: ProtoOSBundleSource.PROTO_OS_BUNDLE_SOURCE_PACKAGE,
        servedReleaseTag: "v0.6.1",
      },
      ...overrides,
    });

  it("ignores the controller software checks that don't apply to a ProtoOS install", () => {
    expect(support(ready({ helperAvailable: false, etcdMembersTotal: 3, etcdMembersHealthy: 1 }))).toEqual({
      summary: "ProtoOS v0.6.1",
      problems: [],
      note: undefined,
    });
  });

  it("says when a controller doesn't accept ProtoOS from Fleet, with its own message", () => {
    expect(support(ready({ protoOs: { selfSupported: false, supportMessage: "frigate 1.2 is too old" } }))).toEqual({
      summary: "ProtoOS version unknown",
      problems: ["A controller doesn't accept ProtoOS updates from Fleet. Update its controller software first."],
      note: "frigate 1.2 is too old",
    });
  });

  it("flags a ProtoOS install that is still running on the controller", () => {
    const busy = ready({
      protoOs: {
        selfSupported: true,
        job: { jobId: "job-0", state: ProtoOSJobState.PROTO_OS_JOB_STATE_INSTALLING },
      },
    });
    expect(support(busy).problems).toEqual(["An update is already running on this controller."]);
    const done = ready({
      protoOs: { selfSupported: true, job: { jobId: "job-0", state: ProtoOSJobState.PROTO_OS_JOB_STATE_FAILED } },
    });
    expect(support(done).problems).toEqual([]);
  });

  it("names the ProtoOS that the controller serves", () => {
    const served = (source: ProtoOSBundleSource) =>
      support(ready({ protoOs: { selfSupported: true, servedSource: source } })).summary;
    expect(served(ProtoOSBundleSource.PROTO_OS_BUNDLE_SOURCE_BUNDLED)).toBe("ProtoOS from the controller software");
    expect(served(ProtoOSBundleSource.PROTO_OS_BUNDLE_SOURCE_NONE)).toBe("No ProtoOS");
    expect(served(ProtoOSBundleSource.PROTO_OS_BUNDLE_SOURCE_UNKNOWN)).toBe("ProtoOS version unknown");
  });

  it("reports only the read failure when the controller could not be read", () => {
    expect(support(create(InstanceSupportSchema, { instanceId: "a", error: "timed out" })).problems).toEqual([
      "Fleet couldn't read this controller: timed out",
    ]);
  });
});
