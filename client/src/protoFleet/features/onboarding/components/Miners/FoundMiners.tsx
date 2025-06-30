import { Dispatch, SetStateAction, useEffect, useMemo, useState } from "react";
import clsx from "clsx";
import FoundMinersModal from "./FoundMinersModal";
import type { MinerWithModel } from "./types";
import { type Device } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import Row from "@/shared/components/Row";
import { minerDiscoveryModes } from "@/shared/components/Setup/miners.constants";

type FoundMinersProps = {
  miners: Device[];
  deselectedMiners: Device["deviceIdentifier"][];
  setDeselectedMiners: Dispatch<SetStateAction<Device["deviceIdentifier"][]>>;
  className?: string;
  minerDiscoveryMode: string;
  handleContinueSetup: (selectedMinerIdentifiers: string[]) => void;
  handleRescanNetwork: () => void;
  handleClearMiners: () => void;
};

type MinersByModel = {
  [key: string]: MinersByModelItem;
};

type MinersByModelItem = {
  model: string;
  miners: MinerWithModel[];
};

const FoundMiners = ({
  miners,
  deselectedMiners,
  setDeselectedMiners,
  className,
  minerDiscoveryMode,
  handleContinueSetup,
  handleRescanNetwork,
  handleClearMiners,
}: FoundMinersProps) => {
  const [minersByModel, setMinersByModel] = useState<MinersByModel>({});
  const [showModal, setShowModal] = useState<boolean>(false);

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

  // flatten minersByModel into list of miners sorted by model and add selected state
  const sortedMinersWithSelection = useMemo(() => {
    const miners = Object.values(minersByModel).flatMap(
      (model) => model.miners,
    );
    return miners.map((miner) => ({
      ...miner,
      selected: !deselectedMiners.includes(miner.deviceIdentifier),
    }));
  }, [minersByModel, deselectedMiners]);

  const selectedMinerIdentifiers = useMemo(() => {
    return sortedMinersWithSelection
      .filter((miner) => miner.selected)
      .map((miner) => miner.deviceIdentifier);
  }, [sortedMinersWithSelection]);

  const totalSelected = selectedMinerIdentifiers.length;

  const models = useMemo(() => {
    return Object.keys(minersByModel).map((model) => model);
  }, [minersByModel]);

  return (
    <div className={clsx("mx-auto flex flex-col gap-6", className)}>
      <div className="rounded-3xl border-1 border-core-primary-5 p-6">
        <div className="mb-4">
          <Header
            inline
            title={
              sortedMinersWithSelection.length === 0
                ? "No miners found so far"
                : `${sortedMinersWithSelection.length} miners found on your network`
            }
            titleSize="text-heading-200"
            description={
              <>
                {sortedMinersWithSelection.length === 0
                  ? "Once some miners are found, you can select the ones you want to configure."
                  : "Select the miners that you want to configure now."}
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
                <Button
                  variant={variants.secondary}
                  size={sizes.compact}
                  onClick={() => setShowModal(true)}
                >
                  {
                    model.miners.filter(
                      (miner) =>
                        !deselectedMiners.includes(miner.deviceIdentifier),
                    ).length
                  }{" "}
                  miners
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
            onClick={() => {
              setMinersByModel({});
              handleClearMiners();
            }}
          >
            Clear found miners
          </Button>
        )}
        {minerDiscoveryMode === minerDiscoveryModes.scan && (
          <Button
            variant={variants.secondary}
            size={sizes.base}
            onClick={handleRescanNetwork}
          >
            Rescan network
          </Button>
        )}
        <Button
          onClick={() => handleContinueSetup(selectedMinerIdentifiers)}
          variant={variants.primary}
          size={sizes.base}
          disabled={totalSelected === 0}
        >
          Continue with {totalSelected} miners
        </Button>
      </div>
      {showModal && (
        <FoundMinersModal
          setDeselectedMiners={setDeselectedMiners}
          miners={sortedMinersWithSelection}
          models={models}
          onDismiss={() => setShowModal(false)}
        />
      )}
    </div>
  );
};

export default FoundMiners;
