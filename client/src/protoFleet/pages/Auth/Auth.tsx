import LoginForm from "@/protoFleet/components/LoginModal/LoginForm";
import { useNavigate } from "@/shared/hooks/useNavigate";

const Auth = () => {
  const navigate = useNavigate();

  return (
    <div className="flex h-screen w-full items-center-safe justify-center-safe p-10">
      <LoginForm
        onSuccess={() => navigate("/")}
        onClickForgotPassword={() => navigate("/signup")}
      />
    </div>
  );
};

export default Auth;
