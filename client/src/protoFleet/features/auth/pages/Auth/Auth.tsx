import LoginForm from "@/protoFleet/features/auth/components/LoginModal/LoginForm";
import { useNavigate } from "@/shared/hooks/useNavigate";

const Auth = () => {
  const navigate = useNavigate();

  // TODO forgot password button does nothing
  return (
    <div className="flex h-screen w-full items-center-safe justify-center-safe bg-surface-base">
      <div className="w-[80%] max-w-100 rounded-3xl bg-surface-elevated-base p-6 shadow-200">
        <LoginForm
          onSuccess={() => navigate("/")}
          onClickForgotPassword={() => null}
          onClickCreateAccount={() => navigate("/welcome")}
        />
      </div>
    </div>
  );
};

export default Auth;
