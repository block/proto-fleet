import { useEffect, useMemo, useState } from "react";
import clsx from "clsx";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import Row from "@/shared/components/Row";

type Miner = {
  deviceIdentifier: string;
  macAddress: string;
};

type FoundMinersProps = {
  miners: Miner[];
  className?: string;
  handleContinueSetup: () => void;
  handleRestartSearch: () => void;
};

type MinersByModel = {
  [key: string]: MinersByModelItem;
};

type MinersByModelItem = {
  model: string;
  miners: (Miner & { model: string; selected: boolean })[];
};

const FoundMiners = ({
  miners,
  className,
  handleContinueSetup,
  handleRestartSearch,
}: FoundMinersProps) => {
  const [minersByModel, setMinersByModel] = useState<MinersByModel>({});

  useEffect(() => {
    const getUpdatedMinersByModel = (prev: MinersByModel) => {
      const _minersByModel: MinersByModel = { ...prev };

      // TODO: Until MDK gives us the model name we'll just set them all to "Proto Rig"
      const minersWithModel = miners.map((miner) => ({
        ...miner,
        model: "Proto Rig",
      }));

      minersWithModel.forEach((miner) => {
        if (!_minersByModel[miner.model]) {
          _minersByModel[miner.model] = {
            model: miner.model,
            miners: [{ ...miner, selected: true }],
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

        _minersByModel[miner.model].miners.push({ ...miner, selected: true });
      });
      return _minersByModel;
    };

    setMinersByModel((prev) => getUpdatedMinersByModel(prev));
  }, [miners]);

  const totalSelected = useMemo(() => {
    return Object.values(minersByModel).reduce(
      (acc, model) =>
        acc + model.miners.filter((miner) => miner.selected).length,
      0,
    );
  }, [minersByModel]);

  return (
    <div className={clsx("mx-auto flex flex-col gap-6", className)}>
      <div className="rounded-3xl border-1 border-core-primary-5 p-6">
        <div className="mb-4">
          <Header
            inline
            title={`${miners.length} miners found on your network`}
            titleSize="text-heading-200"
            description={
              <>
                Select the miners that you want to configure now.
                <br className="phone:hidden" />
                You can always add more miners to this network later.
              </>
            }
          />
        </div>
        <div>
          <Row className="grid grid-cols-3">
            <div className="text-emphasis-300 text-text-primary-50">Model</div>

            <div className="text-emphasis-300 text-text-primary-50">
              Discovered
            </div>

            <div className="text-emphasis-300 text-text-primary-50">
              Selected to add
            </div>
          </Row>
          {Object.values(minersByModel).map((model, index) => (
            <Row key={index} divider={false} className="grid grid-cols-3">
              <div className="h-6 text-emphasis-300">{model.model}</div>

              <div className="h-6 text-emphasis-300">{model.miners.length}</div>

              <div className="h-6 text-emphasis-300">
                <Button variant={variants.secondary} size={sizes.compact}>
                  {model.miners.filter((miner) => miner.selected).length} miners
                </Button>
              </div>
            </Row>
          ))}
        </div>
      </div>
      <div className="flex justify-end gap-3">
        {miners.length > 1 && (
          <Button
            variant={variants.secondary}
            size={sizes.base}
            onClick={handleRestartSearch}
          >
            Restart miner search
          </Button>
        )}
        <Button
          onClick={handleContinueSetup}
          variant={variants.primary}
          size={sizes.base}
        >
          Continue with {totalSelected} miners
        </Button>
      </div>
    </div>
  );
};

export default FoundMiners;
