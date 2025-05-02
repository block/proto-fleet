import { useCallback, useEffect, useState } from "react";
import clsx from "clsx";
import { SimpleErrorProps } from "apiResponseTypes";
import MiningPools, {
  getEmptyPoolsInfo,
  isValidPool,
  PoolInfo,
} from "@/protoOS/components/MiningPools";
import SettingUp from "@/protoOS/components/OnboardingSettingUp";
import { useAccessToken } from "@/protoOS/contexts/AuthContext";
import { WarnBackupPoolDialog } from "@/protoOS/pages/Onboarding/WarnBackupPoolDialog";
import { WarnDefaultPoolCallout } from "@/protoOS/pages/Onboarding/WarnDefaultPoolCallout";
import { Alert } from "@/shared/assets/icons";
import AnimatedDotsBackground from "@/shared/components/Animation";
import Button from "@/shared/components/Button";
import {
  DismissibleCalloutWrapper,
  intents,
} from "@/shared/components/Callout";
import { SetupHeader } from "@/shared/components/Setup";
import { protoOSSteps } from "@/shared/components/Setup/setupHeader.constants";

const MiningPoolPage = () => {
  const [pools, setPools] = useState<PoolInfo[]>(getEmptyPoolsInfo());
  const [settingUpMiner, setSettingUpMiner] = useState(false);

  const [warnDefaultPool, setWarnDefaultPool] = useState(false);
  const [warnBackupPool, setWarnBackupPool] = useState(false);

  // const navigate = useNavigate();

  const [pausedAction, setPausedAction] = useState(false);

  const [createPoolsError, setCreatePoolsError] = useState<SimpleErrorProps>();
  const { checkAccess } = useAccessToken(pausedAction);

  useEffect(() => {
    if (settingUpMiner && createPoolsError?.status === 422) {
      setSettingUpMiner(false);
      setPausedAction(true);
    }
  }, [createPoolsError?.status, settingUpMiner]);

  const onContinue = useCallback(
    (ignoreBackupPools?: boolean) => {
      // check if default pool has been entered
      const noValidDefaultPool = !isValidPool(pools[0]);
      if (noValidDefaultPool) {
        setWarnDefaultPool(true);
        return;
      }
      // ignore backup pools if indicated by the user
      if (!ignoreBackupPools) {
        // check if at least one backup pool has been entered
        const noValidBackupPool =
          !isValidPool(pools[1]) && !isValidPool(pools[2]);
        if (noValidBackupPool) {
          setWarnBackupPool(true);
          return;
        }
      }
      setPausedAction(true);
      checkAccess();

      // have to reset the error here, otherwise it would cause an infinite cycle
      setCreatePoolsError(undefined);
      setSettingUpMiner(true);
    },
    [pools, checkAccess],
  );

  const onContinueWithoutBackup = useCallback(() => {
    setWarnBackupPool(false);
    onContinue(true);
  }, [onContinue]);

  const onChangePools = useCallback((newPools: PoolInfo[]) => {
    setPools(newPools);
    if (isValidPool(newPools[0])) {
      setWarnDefaultPool(false);
    }
  }, []);

  if (settingUpMiner) {
    return (
      <AnimatedDotsBackground>
        <div className="absolute top-1/2 left-1/2 z-10 -translate-x-1/2 -translate-y-1/2 bg-surface-base p-4">
          <div className="w-[600px]">
            <SettingUp
              pools={pools}
              setCreatePoolsError={setCreatePoolsError}
              onChangeSettingUpMiner={setSettingUpMiner}
            />
          </div>
        </div>
      </AnimatedDotsBackground>
    );
  }

  return (
    <div>
      <SetupHeader steps={protoOSSteps} activeStep="miningPool" />
      <WarnBackupPoolDialog
        onAddBackupPool={() => setWarnBackupPool(false)}
        onContinueWithoutBackup={onContinueWithoutBackup}
        show={warnBackupPool}
      />
      <div className="mx-auto max-w-[640px]">
        <MiningPools
          title="Add your mining pool"
          onChange={onChangePools}
          pools={pools}
        >
          <WarnDefaultPoolCallout
            onDismiss={() => setWarnDefaultPool(false)}
            show={warnDefaultPool}
          />
          <DismissibleCalloutWrapper
            className={clsx({
              "mb-10!": createPoolsError?.error !== undefined,
            })}
            icon={<Alert />}
            // TODO intent here has no effect, because callout doesn't have a header
            intent={intents.danger}
            show={createPoolsError?.error !== undefined}
            title={createPoolsError?.error}
            onDismiss={() => setCreatePoolsError(undefined)}
          />
        </MiningPools>
        <Button
          onClick={() => {
            onContinue(false);
          }}
          variant="primary"
          className="mt-4 ml-auto"
        >
          Continue
        </Button>
      </div>
    </div>
  );
};

export default MiningPoolPage;
