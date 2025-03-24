import { useCallback, useEffect, useState } from "react";

import HashboardRow from "./HashboardRow";
import { useHashboardTemperature } from "@/protoOS/api";
import { TemperatureResponseTemperaturedata } from "@/protoOS/api/types";

import InfoWidget from "@/protoOS/components/InfoWidget";
import { variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import Modal from "@/shared/components/Modal";
import { useNavigate } from "@/shared/hooks/useNavigate";
import { getDisplayValue } from "@/shared/utils/stringUtils";

interface TempModalProps {
  duration: TemperatureResponseTemperaturedata["duration"];
  hashboardSerials: string[];
  highestTemp?: number;
  onDismiss: () => void;
  temp?: number;
}

const TempModal = ({
  duration,
  hashboardSerials,
  highestTemp,
  onDismiss,
  temp,
}: TempModalProps) => {
  const navigate = useNavigate();
  const [hashboard1Temperature, setHashboard1Temperature] = useState<number>();
  const [hashboard2Temperature, setHashboard2Temperature] = useState<number>();
  const [hashboard3Temperature, setHashboard3Temperature] = useState<number>();

  const {
    data: hashboard1TemperatureData,
    pending: pendingHashboard1Temperature,
  } = useHashboardTemperature({
    duration,
    hashboardSerial: hashboardSerials?.[0],
    poll: true,
  });
  const {
    data: hashboard2TemperatureData,
    pending: pendingHashboard2Temperature,
  } = useHashboardTemperature({
    duration,
    hashboardSerial: hashboardSerials?.[1],
    poll: true,
  });
  const {
    data: hashboard3TemperatureData,
    pending: pendingHashboard3Temperature,
  } = useHashboardTemperature({
    duration,
    hashboardSerial: hashboardSerials?.[2],
    poll: true,
  });

  useEffect(() => {
    if (
      Array.isArray(hashboard1TemperatureData?.data) &&
      hashboard1TemperatureData?.data?.length
    ) {
      setHashboard1Temperature(
        hashboard1TemperatureData.data?.[
          hashboard1TemperatureData.data.length - 1
        ].value,
      );
    }
  }, [hashboard1TemperatureData]);

  useEffect(() => {
    if (
      Array.isArray(hashboard2TemperatureData?.data) &&
      hashboard2TemperatureData?.data?.length
    ) {
      setHashboard2Temperature(
        hashboard2TemperatureData.data?.[
          hashboard2TemperatureData.data.length - 1
        ].value,
      );
    }
  }, [hashboard2TemperatureData]);

  useEffect(() => {
    if (
      Array.isArray(hashboard3TemperatureData?.data) &&
      hashboard3TemperatureData?.data?.length
    ) {
      setHashboard3Temperature(
        hashboard3TemperatureData.data?.[
          hashboard3TemperatureData.data.length - 1
        ].value,
      );
    }
  }, [hashboard3TemperatureData]);

  const handleClickViewAsics = useCallback(() => {
    onDismiss();
    navigate("/temperature");
  }, [navigate, onDismiss]);

  return (
    <Modal
      buttons={[
        {
          text: "View ASICs",
          onClick: handleClickViewAsics,
          variant: variants.secondary,
        },
        {
          text: "Done",
          variant: variants.primary,
        },
      ]}
      contentHeader="Miner temperature"
      onDismiss={onDismiss}
    >
      <div className="space-y-6">
        <div>
          Proto ASICs are most performant around 50ºc - 90ºc and the miner will
          auto-tune to optimize performance. If temperatures go beyond 90ºc, the
          miner will no longer be able to mine.
        </div>
        <div className="flex">
          <InfoWidget
            title="Current miner temperature"
            value={
              temp &&
              // \u00B0c is the degree symbol
              `${getDisplayValue(temp)}\u00B0c`
            }
          />
          <InfoWidget
            title={`${duration?.toUpperCase()} highest temperature`}
            value={highestTemp && `${getDisplayValue(highestTemp)}\u00B0c`}
          />
        </div>
        <div>
          <Header
            title="Current hashboard temperatures"
            titleSize="text-heading-50"
          />
          {/* TODO: show warning based on how many chips are overheating on this hashboard */}
          {hashboardSerials?.[0] ? (
            <HashboardRow
              label="Hashboard 1"
              secondaryLabel={
                hashboard1Temperature
                  ? `${getDisplayValue(hashboard1Temperature)}\u00B0c`
                  : undefined
              }
              divider={!!hashboardSerials?.[1] || !!hashboardSerials?.[2]}
              loading={pendingHashboard1Temperature}
              // secondaryLabel="75.56ºc • 12 chips are over heating"
              // warn
            />
          ) : null}
          {hashboardSerials?.[1] ? (
            <HashboardRow
              label="Hashboard 2"
              secondaryLabel={
                hashboard2Temperature
                  ? `${getDisplayValue(hashboard2Temperature)}\u00B0c`
                  : undefined
              }
              divider={!!hashboardSerials?.[2]}
              loading={pendingHashboard2Temperature}
            />
          ) : null}
          {hashboardSerials?.[2] ? (
            <HashboardRow
              label="Hashboard 3"
              secondaryLabel={
                hashboard3Temperature
                  ? `${getDisplayValue(hashboard3Temperature)}\u00B0c`
                  : undefined
              }
              divider={false}
              className="-mb-4"
              loading={pendingHashboard3Temperature}
            />
          ) : null}
        </div>
      </div>
    </Modal>
  );
};

export default TempModal;
