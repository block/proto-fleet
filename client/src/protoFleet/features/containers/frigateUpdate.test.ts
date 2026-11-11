import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import {
  describeFrigateError,
  frigateAgentPhaseLabel,
  frigateCodeText,
  frigateRolloutPhaseLabel,
  frigateStateLabel,
  instanceSupportProblems,
} from "./frigateUpdate";
import {
  FrigateAgentPhase,
  FrigateInstallationType,
  FrigateRolloutPhase,
  FrigateUpdateState,
  InstanceSupportSchema,
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
    expect(frigateAgentPhaseLabel(FrigateAgentPhase.STOPPED)).toBe("Frigate stopped");
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
