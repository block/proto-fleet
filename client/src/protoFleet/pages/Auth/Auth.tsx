import LoginForm from "@/protoFleet/components/LoginModal/LoginForm";
import { useNavigate } from "@/shared/hooks/useNavigate";

const Auth = () => {
  const navigate = useNavigate();

  return (
    <div className="items-center-safe·justify-center-safe·flex·h-screen·w-full">
      <LoginForm
        onSuccess={() => navigate("/")}
        onClickForgotPassword={() => navigate("/signup")}
      />
    </div>
  );
};

export default Auth;
