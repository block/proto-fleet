import { useMemo } from "react";
import { useHashboards } from "@/protoOS/api";
import MetadataRow from "@/protoOS/features/diagnostic/components/MetadataRow";
import AsicTablePreview from "@/protoOS/features/kpis/components/Temperature/HbTempPreview/AsicTablePreview";
import {
  useHashboardSlot,
  useMinerHashboard,
  useTemperatureUnit,
} from "@/protoOS/store";
import { Hashboard } from "@/shared/assets/icons";
import Header from "@/shared/components/Header";
import Modal from "@/shared/components/Modal";
import Stats, { type StatsProps } from "@/shared/components/Stats";
import TemperatureValue from "@/shared/components/TemperatureValue";
import { type TemperatureUnit } from "@/shared/features/preferences";
import {
  convertGigahashSecToTerahashSec,
  convertWtoKW,
  getAsicTempValue,
} from "@/shared/utils/utility";

const getStats = (
  avgAsicTemp: number | null | undefined,
  maxAsicTemp: number | null | undefined,
  powerUsage: number | null | undefined,
  hashrateGhs: number | null | undefined,
  temperatureUnit: TemperatureUnit,
): StatsProps["stats"] => {
  const isFahrenheit = temperatureUnit === "F";
  const unit = isFahrenheit ? "ºF" : "ºC";

  return [
    {
      label: "Highest ASIC temp",
      value: getAsicTempValue(maxAsicTemp ?? undefined, isFahrenheit),
      units: maxAsicTemp ? unit : undefined,
    },
    {
      label: "Avg ASIC temp",
      value: getAsicTempValue(avgAsicTemp ?? undefined, isFahrenheit),
      units: avgAsicTemp ? unit : undefined,
    },
    {
      label: "Board power usage",
      value: powerUsage ? convertWtoKW(powerUsage) : undefined,
      units: "kW",
    },
    {
      label: "Board hashrate",
      value: hashrateGhs
        ? convertGigahashSecToTerahashSec(hashrateGhs)
        : undefined,
      units: "TH/S",
    },
  ];
};

interface HashboardInfoModalProps {
  serial: string;
  onDismiss: () => void;
}

function HashboardInfoModal({ serial, onDismiss }: HashboardInfoModalProps) {
  const temperatureUnit = useTemperatureUnit();

  // Get hashboard data from store (combines hardware + telemetry)
  const hashboardData = useMinerHashboard(serial);
  const slotNumber = useHashboardSlot(serial);

  const { data: hashboardsInfo } = useHashboards();

  // Get hashboard metadata
  const hashboardMetadata = useMemo(() => {
    if (!hashboardsInfo) return null;
    const hashboard = hashboardsInfo.find((hboard) => hboard.hb_sn === serial);
    return {
      name: `Hashboard ${slotNumber}`,
      model: hashboard?.board,
      serialNumber: serial,
      slot: slotNumber,
      firmwareVersion: hashboard?.firmware?.version,
      firmwareBuild: hashboard?.firmware?.build,
      firmwareHash: hashboard?.firmware?.git_hash,
      bootloaderVersion: hashboard?.bootloader?.version,
      chipId: hashboard?.chip_id,
      miningAsic: hashboard?.mining_asic,
      asicCount: hashboard?.mining_asic_count,
      port: hashboard?.port,
      apiVersion: hashboard?.api_version,
    };
  }, [hashboardsInfo, serial, slotNumber]);

  return (
    <Modal
      title={"Hashboard status"}
      onDismiss={onDismiss}
      size="large"
      buttons={[
        {
          text: "Done",
          variant: "primary",
          onClick: onDismiss,
        },
      ]}
    >
      <div className="flex flex-col gap-y-6 py-6">
        <Header
          icon={
            <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-core-primary-5">
              <Hashboard />
            </div>
          }
          title={hashboardMetadata?.name}
          titleSize="text-heading-300"
        />
        <div>
          {/* Stats */}
          <div className="mb-6">
            <Stats
              stats={getStats(
                hashboardData?.avgAsicTemp?.latest?.value,
                hashboardData?.maxAsicTemp?.latest?.value,
                hashboardData?.power?.latest?.value,
                hashboardData?.hashrate?.latest?.value,
                temperatureUnit,
              )}
              size="medium"
              gap="gap-6"
              padding="pb-4"
            />
          </div>

          {/* Temperature indicators */}
          {(hashboardData?.inletTemp?.latest?.value ||
            hashboardData?.outletTemp?.latest?.value) && (
            <div className="mb-6">
              <div className="relative flex items-center justify-between font-mono text-mono-text-50 text-text-primary-50 before:absolute before:top-[50%] before:left-0 before:h-[1px] before:w-full before:bg-border-5">
                <div className="relative bg-surface-base pr-4">
                  Front{" "}
                  {hashboardData?.inletTemp?.latest?.value && (
                    <TemperatureValue
                      value={hashboardData.inletTemp.latest.value}
                    />
                  )}
                </div>
                <div className="relative bg-surface-base px-4">{serial}</div>
                <div className="relative bg-surface-base pl-4">
                  Rear{" "}
                  {hashboardData?.outletTemp?.latest?.value && (
                    <TemperatureValue
                      value={hashboardData.outletTemp.latest.value}
                    />
                  )}
                </div>
              </div>
            </div>
          )}
          {/* ASIC Table */}
          <AsicTablePreview hashboardSerial={serial} />
        </div>

        <div className="flex flex-col">
          <MetadataRow label="Serial number" value={serial} />
          {hashboardMetadata?.model ? (
            <MetadataRow label="Model" value={hashboardMetadata.model} />
          ) : null}
          {hashboardMetadata?.firmwareVersion ? (
            <MetadataRow
              label="Firmware version"
              value={hashboardMetadata.firmwareVersion}
            />
          ) : null}
          {hashboardMetadata?.asicCount !== undefined ? (
            <MetadataRow
              label="ASIC count"
              value={hashboardMetadata.asicCount.toString()}
            />
          ) : null}
          {hashboardMetadata?.slot ? (
            <MetadataRow
              label="Slot location"
              value={hashboardMetadata.slot.toString()}
            />
          ) : null}
        </div>
      </div>
    </Modal>
  );
}

export default HashboardInfoModal;
