import { Fragment, useEffect, useMemo, useRef, useState } from "react";
import clsx from "clsx";
import { deviceGroupLabel, entityNoun, foundSummary } from "./foundMinersLabels";
import type { MinerWithModel } from "./types";
import { AuthenticationMethod } from "@/protoFleet/api/generated/capabilities/v1/capabilities_pb";
import { DeviceClass } from "@/protoFleet/api/generated/common/v1/common_pb";
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

type MinersByModelItem = {
  model: string;
  deviceClass: DeviceClass;
  manufacturer: string;
  supportedAuthenticationMethods: AuthenticationMethod[];
  miners: MinerWithModel[];
};

function isProtoRig(manufacturer: string): boolean {
  return manufacturer === "Proto";
}

function supportsAutoAuth(manufacturer: string, supportedMethods: AuthenticationMethod[]): boolean {
  return isProtoRig(manufacturer) && supportedMethods.includes(AuthenticationMethod.BASIC);
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
      const minerKey = JSON.stringify([miner.manufacturer || "unknown", miner.model || "unknown", miner.deviceClass]);

      if (!_minersByModel[minerKey]) {
        const supportedMethods = miner.capabilities?.authentication?.supportedMethods || [];

        _minersByModel[minerKey] = {
          model: miner.model,
          deviceClass: miner.deviceClass,
          manufacturer: miner.manufacturer || "unknown",
          supportedAuthenticationMethods: supportedMethods,
          miners: [miner],
        };
      } else {
        _minersByModel[minerKey].miners.push(miner);
      }
    });

    return _minersByModel;
  }, [miners]);

  const modelEntries = Object.values(minersByModel).sort(
    (a, b) => Number(b.deviceClass === DeviceClass.CONTAINER) - Number(a.deviceClass === DeviceClass.CONTAINER),
  );

  return (
    <div className={clsx("mx-auto flex flex-col gap-6", className)}>
      <div className="mb-4">
        <Header
          inline
          title={(() => {
            const containerCount = modelEntries.reduce(
              (total, item) => total + (item.deviceClass === DeviceClass.CONTAINER ? item.miners.length : 0),
              0,
            );
            const minerCount = miners.length - containerCount;
            if (miners.length === 0 && skeletonVisible) return "Finding devices on your network";
            if (miners.length === 0) return "No devices found";
            if (isScanning) return `Scanning… ${foundSummary(containerCount, minerCount)}`;
            return foundSummary(containerCount, minerCount);
          })()}
          titleSize="text-heading-300"
          description={
            miners.length === 0 && skeletonVisible ? undefined : (
              <>
                {miners.length === 0
                  ? "Try rescanning or check that your devices are powered on and connected to the network."
                  : "Specify which devices to add to your fleet. All devices are selected by default."}
                <br className="phone:hidden" />
                You can always add more devices to this network later.
              </>
            )
          }
        />
      </div>
      {skeletonVisible || modelEntries.length > 0 ? (
        <div className="rounded-3xl border-1 border-core-primary-5 p-6" data-testid="found-miners-list">
          <div>
            <CollapsibleSkeleton visible={skeletonVisible} showDivider={modelEntries.length > 0} />
            {modelEntries.map((model, index) => (
              <Fragment key={JSON.stringify([model.deviceClass, model.manufacturer, model.model])}>
                {index === 0 ||
                (modelEntries[index - 1].deviceClass === DeviceClass.CONTAINER) !==
                  (model.deviceClass === DeviceClass.CONTAINER) ? (
                  <h3 className="pt-3 text-emphasis-300">
                    {model.deviceClass === DeviceClass.CONTAINER ? "Containers" : "Miners"}
                  </h3>
                ) : null}
                <Row divider={false} className="flex items-center justify-between" testId="miner-model-row">
                  <div className="flex gap-4">
                    {isProtoRig(model.manufacturer) ? <LogoAlt width="w-[20px]" /> : <Fleet width="w-[20px]" />}
                    <div>
                      <div className="h-6 text-emphasis-300">
                        {deviceGroupLabel(model.manufacturer, model.model, model.deviceClass)}
                      </div>
                      {model.deviceClass === DeviceClass.CONTAINER &&
                      model.supportedAuthenticationMethods.length === 0 ? null : supportsAutoAuth(
                          model.manufacturer,
                          model.supportedAuthenticationMethods,
                        ) ? (
                        <div className="text-200 text-text-primary-70">
                          Will try the default username/password when added
                        </div>
                      ) : (
                        <div className="text-200 text-text-primary-70">You will need to log in after setup</div>
                      )}
                    </div>
                  </div>

                  <div className="h-6 text-emphasis-300">
                    {(() => {
                      const selectedCount = model.miners.filter(
                        (miner) => !deselectedMiners.includes(miner.deviceIdentifier),
                      ).length;
                      return `${selectedCount} ${entityNoun(model.deviceClass, selectedCount)}`;
                    })()}
                  </div>
                </Row>
                {modelEntries.length > index + 1 ? <Divider /> : null}
              </Fragment>
            ))}
          </div>
        </div>
      ) : null}
    </div>
  );
};

export default FoundMiners;
