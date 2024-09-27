import { useEffect, useMemo } from "react";

import { useSystemStatus } from "api";

import { useLocalStorage } from "common/hooks/useLocalStorage";
import { useNavigate } from "common/hooks/useNavigate";

import Spinner from "components/Spinner";

import Onboarding from "./Onboarding";

const OnboardingWrapper = () => {
  const { data: systemStatus, pending: pendingSystemStatus } =
    useSystemStatus();
  const navigate = useNavigate();
  const { getItem } = useLocalStorage();

  const isOnboarded = useMemo(() => getItem("isOnboarded"), [getItem]);

  // navigate to home page if miner has already been onboarded
  useEffect(() => {
    if (isOnboarded || systemStatus?.onboarded) {
      navigate("/");
    } else if (systemStatus?.password_set === false) {
      navigate("/auth");
    }
  }, [isOnboarded, navigate, systemStatus]);

  return (
    <>
      {(isOnboarded === undefined || isOnboarded) &&
      pendingSystemStatus &&
      systemStatus?.onboarded === undefined ? (
        <div className="min-h-screen flex items-center justify-center">
          <Spinner />
        </div>
      ) : (
        <Onboarding />
      )}
    </>
  );
};

export default OnboardingWrapper;
