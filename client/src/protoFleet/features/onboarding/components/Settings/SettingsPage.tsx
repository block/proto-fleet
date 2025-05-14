import { useCallback, useState } from "react";
import { useNavigate } from "react-router-dom";
import { create } from "@bufbuild/protobuf";
import { CreatePoolRequestSchema } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import useFleet from "@/protoFleet/api/useFleet";
import { STEP_KEYS, STEPS } from "@/protoFleet/features/onboarding/constants";

// TODO: should not be importing from protoOS
import { statuses } from "@/protoOS/components/OnboardingSettingUp/constants";
import OnboardingSettingUp from "@/protoOS/components/OnboardingSettingUp/OnboardingSettingUp";
import { WarnDefaultPoolCallout } from "@/protoOS/features/onboarding/components/WarnDefaultPoolCallout";

import AnimatedDotsBackground from "@/shared/components/Animation";
import Button from "@/shared/components/Button";
import PoolForm from "@/shared/components/MiningPools/PoolForm";
import { PoolInfo } from "@/shared/components/MiningPools/types";
import {
  getEmptyPoolsInfo,
  isValidPool,
} from "@/shared/components/MiningPools/utility";
import { OnboardingLayout } from "@/shared/components/Setup";

// TODO we can probably share more code with ProtoOS
const MiningPoolPage = () => {
  const navigate = useNavigate();
  const { createPool } = useFleet();

  const [pools, setPools] = useState<PoolInfo[]>(getEmptyPoolsInfo());
  const [settingUpMiner, setSettingUpMiner] = useState(false);
  const [poolStatus, setPoolStatus] = useState<keyof typeof statuses>(
    statuses.pending,
  );

  const [warnDefaultPool, setWarnDefaultPool] = useState(false);

  const onContinue = useCallback(() => {
    // check if default pool has been entered
    const noValidDefaultPool = !isValidPool(pools[0]);
    if (noValidDefaultPool) {
      setWarnDefaultPool(true);
      return;
    }

    setSettingUpMiner(true);
    const defaultPool = pools[0];
    const createPoolRequest = create(CreatePoolRequestSchema, {
      poolConfig: {
        url: defaultPool.url,
        username: defaultPool.username,
        password: defaultPool.password,
      },
    });
    createPool({
      createPoolRequest,
      onSuccess: () => setPoolStatus(statuses.success),
      onError: () => setPoolStatus(statuses.error),
    });
  }, [pools, createPool]);

  const onChangePools = useCallback((newPools: PoolInfo[]) => {
    setPools(newPools);
    if (isValidPool(newPools[0])) {
      setWarnDefaultPool(false);
    }
  }, []);

  const handleClickRetry = useCallback(() => {
    setPoolStatus(statuses.fetch);
  }, []);

  const handleClickContinue = useCallback(() => navigate("/"), [navigate]);

  const handleClickReconfigure = useCallback(
    () => setSettingUpMiner(false),
    [setSettingUpMiner],
  );

  if (settingUpMiner) {
    return (
      <AnimatedDotsBackground>
        <div className="absolute top-1/2 left-1/2 z-10 -translate-x-1/2 -translate-y-1/2 bg-surface-base p-4">
          <div className="w-[600px]">
            <OnboardingSettingUp
              poolStatus={poolStatus}
              isSetupDone={poolStatus === statuses.success}
              onClickContinue={handleClickContinue}
              onClickReconfigure={handleClickReconfigure}
              onClickRetry={handleClickRetry}
            />
          </div>
        </div>
      </AnimatedDotsBackground>
    );
  }

  // TODO support connection test
  // TODO support backup pools
  return (
    <OnboardingLayout steps={STEPS} currentStep={STEP_KEYS.settings}>
      <div className="mx-auto max-w-[640px]">
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
        <Button onClick={onContinue} variant="primary" className="mt-4 ml-auto">
          Continue
        </Button>
      </div>
    </OnboardingLayout>
  );
};

export default MiningPoolPage;
