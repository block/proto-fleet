import { useCallback, useState } from "react";

import { variants } from "components/Button";
import MiningPools, {
  getEmptyPoolsInfo,
  isValidPool,
  PoolInfo,
} from "components/MiningPools";

import { tabs } from "./constants";
import OnboardingHeader from "./OnboardingHeader";
import OnboardingNavigation from "./OnboardingNavigation";
import SettingUp from "./SettingUp";
import { Tabs } from "./types";
import { WarnBackupPoolDialog } from "./WarnBackupPoolDialog";
import { WarnDefaultPoolCallout } from "./WarnDefaultPoolCallout";

const Onboarding = () => {
  const [pools, setPools] = useState<PoolInfo[]>(getEmptyPoolsInfo());
  const [finalizedPoolUrls, setFinalizedPoolUrls] = useState<string[]>();

  const [warnDefaultPool, setWarnDefaultPool] = useState(false);
  const [warnBackupPool, setWarnBackupPool] = useState(false);

  const [activeTab, setActiveTab] = useState<Tabs>(tabs.pools);
  const [settingUpMiner, setSettingUpMiner] = useState(false);

  const [isMenuOpen, setIsMenuOpen] = useState(false);

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
      // move on to next step
      setFinalizedPoolUrls(pools.map((pool) => pool.url));
      setSettingUpMiner(true);
    },
    [pools]
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

  return (
    <div className="h-screen flex flex-col">
      {settingUpMiner ? (
        <>
          <OnboardingHeader openMenu={() => setIsMenuOpen(true)} />
          <div className="h-screen flex justify-center items-center">
            <div className="w-[600px]">
              <SettingUp pools={pools} />
            </div>
          </div>
        </>
      ) : (
        <>
          <OnboardingHeader
            button={{
              text: "Finish setup",
              onClick: () => onContinue(),
              variant: variants.accent,
              testId: "finish-setup-button",
            }}
            openMenu={() => setIsMenuOpen(true)}
          />
          <WarnBackupPoolDialog
            onAddBackupPool={() => setWarnBackupPool(false)}
            onContinueWithoutBackup={onContinueWithoutBackup}
            show={warnBackupPool}
          />
          <div className="mt-[60px] h-full">
            <OnboardingNavigation
              isVisible={isMenuOpen}
              closeMenu={() => setIsMenuOpen(false)}
              poolUrls={finalizedPoolUrls}
              activeTab={activeTab}
              onChangeActiveTab={setActiveTab}
            />
            <div className="desktop:ml-80 laptop:ml-80 h-full">
              <div className="m-14 tablet:m-6 phone:m-6 flex justify-center h-full">
                <div className="w-[640px]">
                  {activeTab === tabs.pools && (
                    <>
                      <MiningPools onChange={onChangePools} pools={pools}>
                        <WarnDefaultPoolCallout
                          onDismiss={() => setWarnDefaultPool(false)}
                          show={warnDefaultPool}
                        />
                      </MiningPools>
                    </>
                  )}
                </div>
              </div>
            </div>
          </div>
        </>
      )}
    </div>
  );
};

export default Onboarding;
