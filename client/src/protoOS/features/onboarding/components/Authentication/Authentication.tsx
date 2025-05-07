import { useEffect } from "react";
import { useLogin, usePassword, useSystemStatus } from "@/protoOS/api";
import { Authentication, SetupHeader } from "@/shared/components/Setup";
import { protoOSSteps } from "@/shared/components/Setup/setupHeader.constants";
import { useNavigate } from "@/shared/hooks/useNavigate";

const AuthenticationPage = () => {
  const navigate = useNavigate();
  const setPassword = usePassword();
  const login = useLogin();
  const { data: systemStatus, pending: pendingSystemStatus } =
    useSystemStatus();

  useEffect(() => {
    if (!pendingSystemStatus && systemStatus?.onboarded !== undefined) {
      if (systemStatus.password_set) {
        navigate("/onboarding/mining-pool");
      }
    }
  }, [navigate, systemStatus, pendingSystemStatus]);

  function submit(password: string) {
    setPassword({
      password: password,
      onSuccess: () => {
        login({
          password: password,
          onFinally: () => {
            navigate("/onboarding/mining-pool");
          },
        });
      },
    });
  }

  return (
    <div>
      <SetupHeader steps={protoOSSteps} activeStep="authentication" />
      <Authentication submit={submit} />
    </div>
  );
};

export default AuthenticationPage;
