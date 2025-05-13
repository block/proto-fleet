import { useEffect } from "react";
import { useLogin, usePassword, useSystemStatus } from "@/protoOS/api";
import { Authentication, SetupHeader } from "@/shared/components/Setup";
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
      <SetupHeader />
      <Authentication
        submit={submit}
        headline="Create an admin login for your miners"
        description="This password is required to modify performance settings or mining pool configurations for this miner."
      />
    </div>
  );
};

export default AuthenticationPage;
