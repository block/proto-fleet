import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";

import { useHashboardTemperature } from "api";
import { TemperatureResponseTemperaturedata } from "apiTypes";

import { getDisplayValue } from "common/utils/stringUtils";

import { variants } from "components/Button";
import InfoWidget from "components/InfoWidget";
import Modal from "components/Modal";

import {
  mockHashboard1TemperatureData,
  mockHashboard2TemperatureData,
  mockHashboard3TemperatureData,
} from "./constants";
import HashboardRow from "./HashboardRow";

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

  const { data: hashboard1TemperatureData } = useHashboardTemperature({
    duration,
    hashboardSerial: hashboardSerials?.[0],
    poll: true,
  });
  const { data: hashboard2TemperatureData } = useHashboardTemperature({
    duration,
    hashboardSerial: hashboardSerials?.[1],
    poll: true,
  });
  const { data: hashboard3TemperatureData } = useHashboardTemperature({
    duration,
    hashboardSerial: hashboardSerials?.[2],
    poll: true,
  });

  useEffect(() => {
    if (
      hashboard1TemperatureData?.data &&
      hashboard1TemperatureData.data.length
    ) {
      // TODO: remove else when mocks moved to swagger
      const apiData = hashboard1TemperatureData.data[0].datetime
        ? hashboard1TemperatureData
        : mockHashboard1TemperatureData;
      setHashboard1Temperature(apiData.data?.[apiData.data.length - 1].value);
    }
  }, [hashboard1TemperatureData]);

  useEffect(() => {
    if (
      hashboard2TemperatureData?.data &&
      hashboard2TemperatureData.data.length
    ) {
      // TODO: remove else when mocks moved to swagger
      const apiData = hashboard2TemperatureData.data[0].datetime
        ? hashboard2TemperatureData
        : mockHashboard2TemperatureData;
      setHashboard2Temperature(apiData.data?.[apiData.data.length - 1].value);
    }
  }, [hashboard2TemperatureData]);

  useEffect(() => {
    if (
      hashboard3TemperatureData?.data &&
      hashboard3TemperatureData.data.length
    ) {
      // TODO: remove else when mocks moved to swagger
      const apiData = hashboard3TemperatureData.data[0].datetime
        ? hashboard3TemperatureData
        : mockHashboard3TemperatureData;
      setHashboard3Temperature(apiData.data?.[apiData.data.length - 1].value);
    }
  }, [hashboard3TemperatureData]);

  const handleClickViewAsics = useCallback(() => {
    onDismiss();
    navigate("/hardware");
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
            title="Miner temperature"
            value={
              temp &&
              // \u00B0c is the degree symbol
              `${getDisplayValue(temp)}\u00B0c`
            }
          />
          <InfoWidget
            title="Highest temperature"
            value={highestTemp && `${getDisplayValue(highestTemp)}\u00B0c`}
          />
        </div>
        <div>
          {/* TODO: show warning based on how many chips are overheating on this hashboard */}
          {hashboard1Temperature && (
            <HashboardRow
              label="Hashboard 1"
              secondaryLabel={
                hashboard1Temperature
                  ? `${getDisplayValue(hashboard1Temperature)}\u00B0c`
                  : undefined
              }
              divider={!!hashboard2Temperature || !!hashboard3Temperature}
              // secondaryLabel="75.56ºc • 12 chips are over heating"
              // warn
            />
          )}
          {hashboard2Temperature ? (
            <HashboardRow
              label="Hashboard 2"
              secondaryLabel={`${getDisplayValue(hashboard2Temperature)}\u00B0c`}
            />
          ) : null}
          {hashboard3Temperature ? (
            <HashboardRow
              label="Hashboard 3"
              secondaryLabel={`${getDisplayValue(hashboard3Temperature)}\u00B0c`}
              divider={false}
              className="-mb-4"
            />
          ) : null}
        </div>
      </div>
    </Modal>
  );
};

export default TempModal;
