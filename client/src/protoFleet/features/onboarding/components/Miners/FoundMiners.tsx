import { Fragment, useEffect, useState } from "react";
import clsx from "clsx";
import type { MinerWithModel } from "./types";
import { type Device } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import { Fleet, LogoAlt } from "@/shared/assets/icons";
import Divider from "@/shared/components/Divider";
import Header from "@/shared/components/Header";
import Row from "@/shared/components/Row";

type FoundMinersProps = {
  miners: Device[];
  deselectedMiners: Device["deviceIdentifier"][];
  className?: string;
};

type MinersByModel = {
  [key: string]: MinersByModelItem;
};

type MinersByModelItem = {
  model: string;
  miners: MinerWithModel[];
};

function isProtoRig(model: string): boolean {
  return model === "Proto Rig";
}

const FoundMiners = ({
  miners,
  deselectedMiners,
  className,
}: FoundMinersProps) => {
  const [minersByModel, setMinersByModel] = useState<MinersByModel>({});

  useEffect(() => {
    const getUpdatedMinersByModel = (prev: MinersByModel) => {
      const _minersByModel: MinersByModel = { ...prev };

      miners.forEach((miner) => {
        if (!_minersByModel[miner.model]) {
          _minersByModel[miner.model] = {
            model: miner.model,
            miners: [miner],
          };

          return;

          // if miner is already in our state dont add it again
          // so that we dont have duplicates, and can maintain the selected state
        } else if (
          _minersByModel[miner.model].miners.find(
            (m) => m.deviceIdentifier === miner.deviceIdentifier,
          )
        ) {
          return;
        }

        _minersByModel[miner.model].miners.push(miner);
      });
      return _minersByModel;
    };

    setMinersByModel((prev) => getUpdatedMinersByModel(prev));
  }, [miners]);

  return (
    <div className={clsx("mx-auto flex flex-col gap-6", className)}>
      <div className="mb-4">
        <Header
          inline
          title={
            miners.length === 0
              ? "No miners found so far"
              : `${miners.length} miners found on your network`
          }
          titleSize="text-heading-300"
          description={
            <>
              {miners.length === 0
                ? "Once some miners are found, you can select the ones you want to configure."
                : "Specify which miners to add to your fleet. All miners are selected by default."}
              <br className="phone:hidden" />
              You can always add more miners to this network later.
            </>
          }
        />
      </div>
      <div className="rounded-3xl border-1 border-core-primary-5 p-6">
        <div>
          {Object.values(minersByModel).map((model, index) => (
            <Fragment key={index}>
              <Row
                divider={false}
                className="flex items-center justify-between"
              >
                <div className="flex gap-4">
                  {isProtoRig(model.model) ? (
                    <LogoAlt width="w-[20px]" />
                  ) : (
                    <Fleet width="w-[20px]" />
                  )}
                  <div>
                    <div className="h-6 text-emphasis-300">{model.model}</div>
                    {isProtoRig(model.model) ? (
                      <div className="text-200 text-text-primary-70">
                        Authenticated with default username/password
                      </div>
                    ) : (
                      <div className="text-200 text-text-primary-70">
                        You will need to log in after setup
                      </div>
                    )}
                  </div>
                </div>

                <div className="h-6 text-emphasis-300">
                  {
                    model.miners.filter(
                      (miner) =>
                        !deselectedMiners.includes(miner.deviceIdentifier),
                    ).length
                  }{" "}
                  miners
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
