import LoginForm from "@/protoFleet/features/auth/components/LoginModal/LoginForm";
import { useNavigate } from "@/shared/hooks/useNavigate";

const Auth = () => {
  const navigate = useNavigate();

  return (
    <div className="items-center-safe justify-center-safe flex h-screen w-full bg-surface-base">
      <div className="w-[80%] max-w-100 rounded-3xl bg-surface-elevated-base p-6 shadow-200">
        <LoginForm
          onSuccess={() => navigate("/")}
          onClickForgotPassword={() => null}
          onClickCreateAccount={() => navigate("/onboarding/authentication")}
        />
      </div>
    </div>
  );
};

export default Auth;
