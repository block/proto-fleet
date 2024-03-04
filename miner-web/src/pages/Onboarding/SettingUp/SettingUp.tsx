import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import clsx from "clsx";

import { useCoolingMode, useCreatePool, usePoolsInfo } from "api";

import Button, { sizes, variants } from "components/Button";
import Header from "components/Header";

import ArrowRight from "icons/ArrowRight";

import { FanMode, PoolInfo } from "../types";
import { isValidPool } from "../utility";
import { statuses } from "./constants";
import Item from "./Item";

interface SettingUpProps {
  fanMode: FanMode;
  pools: PoolInfo[];
}

const SettingUp = ({ fanMode, pools }: SettingUpProps) => {
  const Navigate = useNavigate();
  const { createPool } = useCreatePool();
  const { setCoolingMode } = useCoolingMode();
  const { fetch: fetchPools } = usePoolsInfo();
  const [intervalId, setIntervalId] =
    useState<ReturnType<typeof setInterval>>();
  const [poolStatus, setPoolStatus] = useState<keyof typeof statuses>(
    statuses.fetch
  );
  const [fanStatus, setFanStatus] = useState<keyof typeof statuses>(
    statuses.fetch
  );

  const getPoolStatus = useCallback(() => {
    fetchPools({
      onSuccess: () => setPoolStatus(statuses.success),
      onError: (error) => {
        // wait for cgminer to restart before marking pools as configured
        const message = (error?.message || "").toLowerCase();
        if (!/failed to connect to cgminer/.test(message)) {
          setPoolStatus(statuses.error);
        }
      },
    });
  }, [fetchPools]);

  useEffect(() => {
    if (poolStatus !== statuses.pending && intervalId) {
      clearInterval(intervalId);
      setIntervalId(undefined);
    }
  }, [intervalId, poolStatus]);

  useEffect(() => {
    if (poolStatus === statuses.fetch) {
      setPoolStatus(statuses.pending);
      const validPools = pools.filter(isValidPool);
      createPool({
        poolInfo: validPools,
        onSuccess: () => {
          const newIntervalId = setInterval(getPoolStatus, 2500);
          setIntervalId(newIntervalId);
        },
        onError: () => setPoolStatus(statuses.error),
      });
    }
  }, [createPool, getPoolStatus, poolStatus, pools]);

  useEffect(() => {
    // TODO: revisit this when API is ready
    if (fanStatus === statuses.fetch) {
      setFanStatus(statuses.pending);
      setCoolingMode({
        fanMode,
        onSuccess: () => setFanStatus(statuses.success),
        onError: () => setFanStatus(statuses.error),
      });
    }
  }, [fanMode, fanStatus, setCoolingMode]);

  const isConfigured = useCallback(
    (status: keyof typeof statuses) =>
      status === statuses.success || status === statuses.error,
    []
  );

  return (
    <>
      <Header
        title="We’re setting up your miner"
        titleSize="text-heading-300"
        subtitle="This may take a few minutes. Please do not close this window."
        subtitleSize="text-300"
        className="mb-3"
      />
      <Item
        status={poolStatus}
        text="mining pools"
        onClickRetry={() => setPoolStatus(statuses.fetch)}
      />
      <Item
        status={fanStatus}
        text="fans"
        onClickRetry={() => setPoolStatus(statuses.fetch)}
      />
      <div className="flex justify-end">
        <Button
          variant={variants.accent}
          size={sizes.base}
          text="Continue to dashboard"
          className={clsx(
            "mt-6 transition-opacity ease-in-out duration-200 opacity-0",
            {
              "opacity-100 animate-[fade-in_.31s_ease-in-out]":
                isConfigured(poolStatus) && isConfigured(fanStatus),
            }
          )}
          suffixIcon={<ArrowRight />}
          onClick={() => Navigate("/")}
          testId="continue-to-dashboard-button"
        />
      </div>
    </>
  );
};

export default SettingUp;
