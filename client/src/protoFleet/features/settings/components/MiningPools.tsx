import { useCallback, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { CreatePoolRequestSchema } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import useFleet from "@/protoFleet/api/useFleet";

import Button from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import PoolForm from "@/shared/components/MiningPools/PoolForm";
import { PoolInfo } from "@/shared/components/MiningPools/types";
import {
  getEmptyPoolsInfo,
  isValidPool,
} from "@/shared/components/MiningPools/utility";
import { WarnDefaultPoolCallout } from "@/shared/components/MiningPools/WarnDefaultPoolCallout";
import { pushToast, STATUSES } from "@/shared/features/toaster";

const MiningPools = () => {
  const { createPool } = useFleet();

  const [pools, setPools] = useState<PoolInfo[]>(getEmptyPoolsInfo());

  const [warnDefaultPool, setWarnDefaultPool] = useState(false);

  const onContinue = useCallback(() => {
    // check if default pool has been entered

    const defaultPool = pools[0];
    const noValidDefaultPool = !isValidPool(defaultPool);
    if (noValidDefaultPool) {
      setWarnDefaultPool(true);
      return;
    }

    const createPoolRequest = create(CreatePoolRequestSchema, {
      poolConfig: {
        url: defaultPool.url,
        username: defaultPool.username,
        password: defaultPool.password,
      },
    });
    createPool({
      createPoolRequest,
      onSuccess: () =>
        pushToast({
          message: "Your default pool has been set",
          status: STATUSES.success,
        }),
      onError: () =>
        pushToast({
          message: "Something went wrong, please try again",
          status: STATUSES.error,
        }),
    });
  }, [pools, createPool]);

  const onChangePools = useCallback((newPools: PoolInfo[]) => {
    setPools(newPools);
    if (isValidPool(newPools[0])) {
      setWarnDefaultPool(false);
    }
  }, []);

  return (
    <>
      <div className="mx-auto flex max-w-xl flex-col gap-6">
        <Header
          title={"Update your mining pools"}
          titleSize="text-heading-300"
          description={"TODO - add description"}
        />
        <div>
          <div className="mb-4 flex items-center">
            <div className="grow text-heading-100 text-text-primary">
              Default pool
            </div>
          </div>
          <WarnDefaultPoolCallout
            onDismiss={() => setWarnDefaultPool(false)}
            show={warnDefaultPool}
          />
          <PoolForm
            poolIndex={0}
            pools={pools}
            onChangePools={onChangePools}
            shouldTestConnection={false}
            testConnection={() => {}}
            isTestingConnection={false}
            setShouldTestConnection={() => {}}
          />
          <Button
            onClick={onContinue}
            variant="primary"
            className="mt-4 ml-auto"
          >
            Continue
          </Button>
        </div>
      </div>
    </>
  );
};

export default MiningPools;
