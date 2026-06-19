import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import FoundMiners from "./FoundMiners";
import {
  AuthenticationCapabilitiesSchema,
  AuthenticationMethod,
  MinerCapabilitiesSchema,
} from "@/protoFleet/api/generated/capabilities/v1/capabilities_pb";
import { DeviceSchema } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";

const miner = ({
  manufacturer,
  supportedMethods,
}: {
  manufacturer: string;
  supportedMethods: AuthenticationMethod[];
}) =>
  create(DeviceSchema, {
    deviceIdentifier: `${manufacturer}-${supportedMethods.join("-")}`,
    ipAddress: "192.168.1.100",
    model: "Rig",
    manufacturer,
    capabilities: create(MinerCapabilitiesSchema, {
      authentication: create(AuthenticationCapabilitiesSchema, { supportedMethods }),
    }),
  });

describe("FoundMiners", () => {
  it("shows default-credential copy for Proto basic-auth miners", () => {
    render(
      <FoundMiners
        miners={[miner({ manufacturer: "Proto", supportedMethods: [AuthenticationMethod.BASIC] })]}
        deselectedMiners={[]}
      />,
    );

    expect(screen.getByText("Authenticated with default username/password")).toBeInTheDocument();
  });

  it("does not infer default credentials from third-party basic-auth support", () => {
    render(
      <FoundMiners
        miners={[miner({ manufacturer: "Bitmain", supportedMethods: [AuthenticationMethod.BASIC] })]}
        deselectedMiners={[]}
      />,
    );

    expect(screen.getByText("You will need to log in after setup")).toBeInTheDocument();
  });

  it("still shows auto-auth copy for asymmetric-key miners", () => {
    render(
      <FoundMiners
        miners={[miner({ manufacturer: "FutureASIC", supportedMethods: [AuthenticationMethod.ASYMMETRIC_KEY] })]}
        deselectedMiners={[]}
      />,
    );

    expect(screen.getByText("Authenticated with default username/password")).toBeInTheDocument();
  });
});
