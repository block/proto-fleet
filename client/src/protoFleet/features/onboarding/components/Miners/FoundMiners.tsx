import { Fragment, useMemo } from "react";
import clsx from "clsx";
import type { MinerWithModel } from "./types";
import { AuthenticationMethod } from "@/protoFleet/api/generated/capabilities/v1/capabilities_pb";
import { type Device } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import { Fleet, LogoAlt } from "@/shared/assets/icons";
import Divider from "@/shared/components/Divider";
import Header from "@/shared/components/Header";
import Row from "@/shared/components/Row";

type FoundMinersProps = {
  miners: Device[];
  deselectedMiners: Device["deviceIdentifier"][];
  isScanning?: boolean;
  className?: string;
};

type MinersByModel = {
  [key: string]: MinersByModelItem;
};

class MinerKey {
  manufacturer: string;
  model: string;

  constructor(manufacturer: string, model: string) {
    this.manufacturer = manufacturer;
    this.model = model;
  }

  toString(): string {
    return `${this.manufacturer}:${this.model}`;
  }
}

type MinersByModelItem = {
  model: string;
  manufacturer: string;
  supportedAuthenticationMethods: AuthenticationMethod[];
  miners: MinerWithModel[];
};

function isProtoRig(manufacturer: string): boolean {
  return manufacturer === "Proto";
}

function supportsAutoAuth(supportedMethods: AuthenticationMethod[]): boolean {
  return supportedMethods.includes(AuthenticationMethod.ASYMMETRIC_KEY);
}

const FoundMiners = ({ miners, deselectedMiners, isScanning, className }: FoundMinersProps) => {
  // Derive minersByModel directly from miners prop
  const minersByModel = useMemo(() => {
    const _minersByModel: MinersByModel = {};

    miners.forEach((miner) => {
      const minerKey = new MinerKey(miner.manufacturer || "unknown", miner.model || "unknown");

      if (!_minersByModel[minerKey.toString()]) {
        const supportedMethods = miner.capabilities?.authentication?.supportedMethods || [];

        _minersByModel[minerKey.toString()] = {
          model: miner.model,
          manufacturer: miner.manufacturer || "unknown",
          supportedAuthenticationMethods: supportedMethods,
          miners: [miner],
        };
      } else if (
        // if miner is already in our state dont add it again
        // so that we dont have duplicates
        !_minersByModel[minerKey.toString()].miners.find((m) => m.ipAddress === miner.ipAddress)
      ) {
        _minersByModel[minerKey.toString()].miners.push(miner);
      }
    });

    return _minersByModel;
  }, [miners]);

  return (
    <div className={clsx("mx-auto flex flex-col gap-6", className)}>
      <div className="mb-4">
        <Header
          inline
          title={(() => {
            const totalMinerCount = Object.values(minersByModel).reduce((total, item) => total + item.miners.length, 0);
            if (miners.length === 0) return "No miners found";
            if (isScanning) return `Finding miners on your network... ${totalMinerCount} found so far`;
            return `${totalMinerCount} miners found on your network`;
          })()}
          titleSize="text-heading-300"
          description={
            <>
              {miners.length === 0
                ? "Try rescanning or check that your miners are powered on and connected to the network."
                : "Specify which miners to add to your fleet. All miners are selected by default."}
              <br className="phone:hidden" />
              You can always add more miners to this network later.
            </>
          }
        />
      </div>
      <div className="rounded-3xl border-1 border-core-primary-5 p-6" data-testid="found-miners-list">
        <div>
          {Object.values(minersByModel).map((model, index) => (
            <Fragment key={index}>
              <Row divider={false} className="flex items-center justify-between" testId="miner-model-row">
                <div className="flex gap-4">
                  {isProtoRig(model.manufacturer) ? <LogoAlt width="w-[20px]" /> : <Fleet width="w-[20px]" />}
                  <div>
                    <div className="h-6 text-emphasis-300">
                      {model.manufacturer} {model.model}
                    </div>
                    {supportsAutoAuth(model.supportedAuthenticationMethods) ? (
                      <div className="text-200 text-text-primary-70">Authenticated with default username/password</div>
                    ) : (
                      <div className="text-200 text-text-primary-70">You will need to log in after setup</div>
                    )}
                  </div>
                </div>

                <div className="h-6 text-emphasis-300">
                  {model.miners.filter((miner) => !deselectedMiners.includes(miner.deviceIdentifier)).length} miners
                </div>
              </Row>
              {Object.values(minersByModel).length > index + 1 && <Divider />}
            </Fragment>
          ))}
        </div>
      </div>
    </div>
  );
};

export default FoundMiners;
