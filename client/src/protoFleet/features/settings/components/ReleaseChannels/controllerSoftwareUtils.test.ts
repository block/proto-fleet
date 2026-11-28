import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";

import {
  containersBehind,
  controllerSoftwareHoldMessage,
  controllerSoftwareOptions,
  controllerSoftwareOptionValue,
  controllerSoftwareStatus,
  controllerSoftwareVersions,
} from "./controllerSoftwareUtils";
import { FrigatePackageKind, FrigatePackageSchema } from "@/protoFleet/api/generated/frigateupdate/v1/frigateupdate_pb";
import { ReleaseChannelControllerSoftwareSchema } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

const software = (targetVersion: string, containerCount: number, onTargetCount: number) =>
  create(ReleaseChannelControllerSoftwareSchema, {
    manufacturer: "Proto",
    model: "CT1",
    targetVersion,
    containerCount,
    onTargetCount,
  });

describe("controllerSoftwareVersions", () => {
  it("offers only controller software packages, never ProtoOS or an unknown kind", () => {
    const uploaded = [
      create(FrigatePackageSchema, { kind: FrigatePackageKind.CONTROLLER_SOFTWARE, version: "1.6.0" }),
      create(FrigatePackageSchema, { kind: FrigatePackageKind.PROTO_OS, version: "0.6.2~internal.14" }),
      create(FrigatePackageSchema, { kind: FrigatePackageKind.UNSPECIFIED, version: "9.9.9" }),
      create(FrigatePackageSchema, { kind: FrigatePackageKind.CONTROLLER_SOFTWARE, version: "1.5.2" }),
    ];

    expect(controllerSoftwareVersions(uploaded)).toEqual(["1.6.0", "1.5.2"]);
    expect(controllerSoftwareOptions(controllerSoftwareVersions(uploaded), "").map((option) => option.label)).toEqual([
      "Not set",
      "1.6.0",
      "1.5.2",
    ]);
  });
});

describe("controller software in a channel", () => {
  it("counts no container as behind when no version is set", () => {
    expect(containersBehind(software("", 4, 0))).toBe(0);
    expect(containersBehind(undefined)).toBe(0);
    expect(containersBehind(software("1.6.0", 4, 1))).toBe(3);
    expect(controllerSoftwareStatus(software("", 4, 0))).toBeNull();
  });

  it("summarizes how many containers are on the version", () => {
    expect(controllerSoftwareStatus(software("1.6.0", 2, 2))).toEqual({ label: "All on 1.6.0", tone: "completed" });
    expect(controllerSoftwareStatus(software("1.6.0", 2000, 1250))).toEqual({
      label: "1,250 of 2,000 on 1.6.0",
      tone: "attention",
    });
    // A version for a model with no containers yet has nothing to report.
    expect(controllerSoftwareStatus(software("1.6.0", 0, 0))).toBeNull();
  });

  it("explains a held firmware update by container count", () => {
    expect(controllerSoftwareHoldMessage(software("1.6.0", 2, 2))).toBeNull();
    expect(controllerSoftwareHoldMessage(software("", 2, 0))).toBeNull();
    expect(controllerSoftwareHoldMessage(software("1.6.0", 2, 1))).toBe(
      "1 container is held until its controller software is 1.6.0.",
    );
    expect(controllerSoftwareHoldMessage(software("1.6.0", 3, 1))).toBe(
      "2 containers are held until their controller software is 1.6.0.",
    );
  });

  it("offers each uploaded version once and keeps a version whose package is gone", () => {
    expect(controllerSoftwareOptions(["1.6.0", "v1.6.0", " 1.5.0 ", ""], "1.6.0")).toEqual([
      { value: "", label: "Not set" },
      { value: "1.6.0", label: "1.6.0" },
      { value: "1.5.0", label: "1.5.0" },
    ]);
    expect(controllerSoftwareOptions(["1.6.0"], "1.4.0")).toEqual([
      { value: "", label: "Not set" },
      { value: "1.6.0", label: "1.6.0" },
      { value: "1.4.0", label: "1.4.0", description: "Package is no longer uploaded" },
    ]);
  });

  it("selects the uploaded spelling of the current version", () => {
    const options = controllerSoftwareOptions(["v1.6.0"], "1.6.0");
    expect(controllerSoftwareOptionValue(options, "1.6.0")).toBe("v1.6.0");
    expect(controllerSoftwareOptionValue(options, "")).toBe("");
  });
});
