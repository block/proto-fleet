import { useEffect, useMemo } from "react";
import { useNavigate } from "react-router-dom";

import { useSystemStatus } from "api";

import { useLocalStorage } from "common/hooks/useLocalStorage";

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
