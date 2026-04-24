import { useCallback, useState } from "react";
import clsx from "clsx";
import { useCoolingStatus } from "@/protoOS/api";
import { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import MiningPools, { getEmptyPoolsInfo, isValidPool, PoolInfo } from "@/protoOS/components/MiningPools";
import SettingUp from "@/protoOS/components/OnboardingSettingUp";
import NoFansDetectedDialog from "@/protoOS/features/onboarding/components/NoFansDetectedDialog";
import { useAccessToken } from "@/protoOS/store";
import { areAllFansDisconnected } from "@/protoOS/store/utils/coolingUtils";
import { Alert } from "@/shared/assets/icons";
import AnimatedDotsBackground from "@/shared/components/Animation";
import ButtonGroup, { groupVariants } from "@/shared/components/ButtonGroup";
import { DismissibleCalloutWrapper, intents } from "@/shared/components/Callout";
import { WarnBackupPoolDialog } from "@/shared/components/MiningPools/WarnBackupPoolDialog";
import { WarnDefaultPoolCallout } from "@/shared/components/MiningPools/WarnDefaultPoolCallout";
import { OnboardingLayout } from "@/shared/components/Setup";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { useNavigate } from "@/shared/hooks/useNavigate";

const MiningPoolPage = () => {
  const [pools, setPools] = useState<PoolInfo[]>(getEmptyPoolsInfo());
  const [settingUpMiner, setSettingUpMiner] = useState(false);

  const [warnDefaultPool, setWarnDefaultPool] = useState(false);
  const [warnBackupPool, setWarnBackupPool] = useState(false);
  const [showNoFansDialog, setShowNoFansDialog] = useState(false);
  const [dialogTriggeredBySkip, setDialogTriggeredBySkip] = useState(false);

  const navigate = useNavigate();

  const [pausedAction, setPausedAction] = useState(false);

  const [createPoolsError, setCreatePoolsError] = useState<ErrorProps>();
  const { checkAccess } = useAccessToken(pausedAction);

  // Check if fans are connected using cooling API
  const { data: coolingData, setCooling, pending: coolingPending, loaded: coolingLoaded } = useCoolingStatus();
  const isCoolingStatusReady = coolingLoaded && !coolingPending;
  const noFansConnected = isCoolingStatusReady && areAllFansDisconnected(coolingData?.fans);

  // Pause setup and prompt reauth when backend responds 422
  const errorStatus = createPoolsError?.status;
  const [prevErrorStatus, setPrevErrorStatus] = useState(errorStatus);
  if (prevErrorStatus !== errorStatus) {
    setPrevErrorStatus(errorStatus);
    if (settingUpMiner && errorStatus === 422) {
      setSettingUpMiner(false);
      setPausedAction(true);
    }
  }

  const proceedWithSetup = useCallback(() => {
    setPausedAction(true);
    checkAccess();

    // have to reset the error here, otherwise it would cause an infinite cycle
    setCreatePoolsError(undefined);
    setSettingUpMiner(true);
  }, [checkAccess]);

  const onContinue = useCallback(
    (ignoreBackupPools?: boolean) => {
      if (!isCoolingStatusReady) return;

      // check if default pool has been entered
      const noValidDefaultPool = !isValidPool(pools[0]);
      if (noValidDefaultPool) {
        setWarnDefaultPool(true);
        return;
      }
      // ignore backup pools if indicated by the user
      if (!ignoreBackupPools) {
        // check if at least one backup pool has been entered
        const noValidBackupPool = !isValidPool(pools[1]) && !isValidPool(pools[2]);
        if (noValidBackupPool) {
          setWarnBackupPool(true);
          return;
        }
      }

      // Check if no fans are connected
      if (noFansConnected) {
        setDialogTriggeredBySkip(false);
        setShowNoFansDialog(true);
        return;
      }

      proceedWithSetup();
    },
    [isCoolingStatusReady, pools, noFansConnected, proceedWithSetup],
  );

  const onContinueWithoutBackup = useCallback(() => {
    setWarnBackupPool(false);
    onContinue(true);
  }, [onContinue]);

  const handleUseAirCooling = useCallback(() => {
    setShowNoFansDialog(false);
    if (dialogTriggeredBySkip) {
      navigate("/");
    } else {
      proceedWithSetup();
    }
  }, [dialogTriggeredBySkip, navigate, proceedWithSetup]);

  const handleConfirmImmersionCooling = useCallback(() => {
    setCooling({
      mode: "Off",
      onSuccess: () => {
        setShowNoFansDialog(false);
        if (dialogTriggeredBySkip) {
          navigate("/");
        } else {
          proceedWithSetup();
        }
      },
      onError: (error) => {
        pushToast({
          message: error?.error?.message ?? "Unable to set cooling mode. Please try again.",
          status: STATUSES.error,
        });
      },
    });
  }, [setCooling, dialogTriggeredBySkip, navigate, proceedWithSetup]);

  const handleSkip = useCallback(() => {
    if (!isCoolingStatusReady) return;

    setCreatePoolsError(undefined);

    // Check if no fans are connected
    if (noFansConnected) {
      setDialogTriggeredBySkip(true);
      setShowNoFansDialog(true);
      return;
    }

    navigate("/");
  }, [isCoolingStatusReady, noFansConnected, navigate]);

  const onChangePools = useCallback((newPools: PoolInfo[]) => {
    setPools(newPools);
    if (isValidPool(newPools[0])) {
      setWarnDefaultPool(false);
    }
  }, []);

  if (settingUpMiner) {
    return (
      <div className="h-svh w-full bg-surface-base">
        <AnimatedDotsBackground>
          <div className="absolute top-1/2 left-1/2 z-10 w-full max-w-[600px] -translate-x-1/2 -translate-y-1/2 bg-surface-base p-4">
            <div>
              <SettingUp
                pools={pools}
                setCreatePoolsError={setCreatePoolsError}
                onChangeSettingUpMiner={setSettingUpMiner}
              />
            </div>
          </div>
        </AnimatedDotsBackground>
      </div>
    );
  }

  return (
    <OnboardingLayout>
      <WarnBackupPoolDialog
        open={warnBackupPool}
        onAddBackupPool={() => setWarnBackupPool(false)}
        onContinueWithoutBackup={onContinueWithoutBackup}
      />
      <NoFansDetectedDialog
        open={showNoFansDialog}
        onUseAirCooling={handleUseAirCooling}
        onConfirmImmersionCooling={handleConfirmImmersionCooling}
        loading={coolingPending}
      />
      <div className="flex w-full flex-col gap-4">
        <MiningPools title="Add your mining pool" onChange={onChangePools} pools={pools}>
          <WarnDefaultPoolCallout onDismiss={() => setWarnDefaultPool(false)} show={warnDefaultPool} />
          <DismissibleCalloutWrapper
            className={clsx({
              "mb-10!": createPoolsError?.error?.message !== undefined,
            })}
            icon={<Alert />}
            // TODO intent here has no effect, because callout doesn't have a header
            intent={intents.danger}
            show={createPoolsError?.error?.message !== undefined}
            title={createPoolsError?.error?.message ?? "An error occurred"}
            onDismiss={() => setCreatePoolsError(undefined)}
          />
        </MiningPools>
        <ButtonGroup
          className="flex justify-end"
          buttons={[
            {
              text: "Skip",
              onClick: handleSkip,
              variant: "secondary",
              disabled: !isCoolingStatusReady,
            },
            {
              text: "Continue",
              onClick: () => {
                onContinue(false);
              },
              variant: "primary",
              disabled: !isCoolingStatusReady,
            },
          ]}
          variant={groupVariants.rightAligned}
        />
      </div>
    </OnboardingLayout>
  );
};

export default MiningPoolPage;
