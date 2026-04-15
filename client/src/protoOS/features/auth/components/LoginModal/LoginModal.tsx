import { useState } from "react";

import ForgotPassword from "./ForgotPassword";
import LoginForm from "./LoginForm";
import ResizeablePanel from "./ResizeablePanel";
import Modal from "@/shared/components/Modal";

interface LoginModalProps {
  onDismiss?: () => void;
  onSuccess: () => void;
}

const LoginModal = ({ onDismiss, onSuccess }: LoginModalProps) => {
  const [showForgotPassword, setShowForgotPassword] = useState(false);

  return (
    <Modal onDismiss={onDismiss} showHeader={false} zIndex="z-70">
      <ResizeablePanel resizeOn={showForgotPassword}>
        {showForgotPassword ? (
          <ForgotPassword key="forgot-password" onDismiss={() => setShowForgotPassword(false)} />
        ) : (
          <LoginForm
            key="login-form"
            onSuccess={onSuccess}
            onDismiss={onDismiss}
            onClickForgotPassword={() => setShowForgotPassword(true)}
          />
        )}
      </ResizeablePanel>
    </Modal>
  );
};

export default LoginModal;
