import { useLogin } from "@/protoFleet/api/useLogin";
import { usePassword } from "@/protoFleet/api/usePassword";
import { Authentication, SetupHeader } from "@/shared/components/Setup";
import {
  protoFleetSteps,
  steps,
} from "@/shared/components/Setup/setupHeader.constants";
import { useNavigate } from "@/shared/hooks/useNavigate";

const AuthenticationPage = () => {
  const setPassword = usePassword();
  const login = useLogin();
  const navigate = useNavigate();

  function submit(password: string) {
    setPassword({
      password: password,
      onSuccess: () => {
        login({
          password: password,
          onFinally: () => {
            navigate("/onboarding/miners");
          },
        });
      },
    });
  }

  return (
    <div>
      <SetupHeader steps={protoFleetSteps} activeStep={steps.authentication} />
      <Authentication submit={submit} />
    </div>
  );
};

export default AuthenticationPage;
