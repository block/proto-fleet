import { Fragment, useEffect, useMemo, useRef, useState } from "react";
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
  /** Whether a network scan is actively in progress (controls title text). */
  isScanning?: boolean;
  /** Whether to show skeleton loading rows (may outlast isScanning due to min display time). */
  showSkeleton?: boolean;
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

const SKELETON_INDICES = [0, 1, 2];

const SkeletonMinerRows = () => (
  <>
    {SKELETON_INDICES.map((index) => (
      <div key={index} className="flex items-center justify-between py-3" data-testid="skeleton-row">
        <div className="flex items-center gap-4">
          <div className="size-5 animate-pulse rounded-full bg-core-primary-20" />
          <div className="flex flex-col gap-3">
            <div className="h-3 w-24 animate-pulse rounded-sm bg-core-primary-20" />
            <div className="h-3 w-60 animate-pulse rounded-sm bg-core-primary-20" />
          </div>
        </div>
        <div className="h-3 w-12 animate-pulse rounded-sm bg-core-primary-20" />
      </div>
    ))}
  </>
);

const CollapsibleSkeleton = ({ visible, showDivider }: { visible: boolean; showDivider: boolean }) => {
  const contentRef = useRef<HTMLDivElement>(null);
  const [height, setHeight] = useState<number | undefined>(undefined);

  useEffect(() => {
    if (visible && contentRef.current) {
      setHeight(contentRef.current.scrollHeight);
    }
  }, [visible, showDivider]);

  return (
    <div
      className="overflow-hidden transition-[max-height,opacity] duration-300 ease-in-out"
      style={{ maxHeight: visible ? height : 0, opacity: visible ? 1 : 0 }}
    >
      <div ref={contentRef}>
        <SkeletonMinerRows />
        {showDivider ? <Divider /> : null}
      </div>
    </div>
  );
};

const FoundMiners = ({ miners, deselectedMiners, isScanning, showSkeleton, className }: FoundMinersProps) => {
  const skeletonVisible = showSkeleton ?? !!isScanning;
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

  const modelEntries = Object.values(minersByModel);

  return (
    <div className={clsx("mx-auto flex flex-col gap-6", className)}>
      <div className="mb-4">
        <Header
          inline
          title={(() => {
            const totalMinerCount = modelEntries.reduce((total, item) => total + item.miners.length, 0);
            if (miners.length === 0 && skeletonVisible) return "Finding miners on your network";
            if (miners.length === 0) return "No miners found";
            if (isScanning) return `Finding miners on your network... ${totalMinerCount} found so far`;
            return `${totalMinerCount} miners found on your network`;
          })()}
          titleSize="text-heading-300"
          description={
            miners.length === 0 && skeletonVisible ? undefined : (
              <>
                {miners.length === 0
                  ? "Try rescanning or check that your miners are powered on and connected to the network."
                  : "Specify which miners to add to your fleet. All miners are selected by default."}
                <br className="phone:hidden" />
                You can always add more miners to this network later.
              </>
            )
          }
        />
      </div>
      <div className="rounded-3xl border-1 border-core-primary-5 p-6" data-testid="found-miners-list">
        <div>
          <CollapsibleSkeleton visible={skeletonVisible} showDivider={modelEntries.length > 0} />
          {modelEntries.map((model, index) => (
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
              {modelEntries.length > index + 1 ? <Divider /> : null}
            </Fragment>
          ))}
        </div>
      </div>
    </div>
  );
};

export default FoundMiners;
