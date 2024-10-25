import { useState } from "react";

import Modal from "components/Modal";

import ForgotPassword from "./ForgotPassword";
import LoginForm from "./LoginForm";

interface LoginModalProps {
  onDismiss?: () => void;
  onSuccess: () => void;
}

const LoginModal = ({ onDismiss, onSuccess }: LoginModalProps) => {
  const [showForgotPassword, setShowForgotPassword] = useState(false);

  return (
    <Modal onDismiss={onDismiss} showHeader={false} className="!w-[402px]">
      {showForgotPassword ? (
        <ForgotPassword onDismiss={() => setShowForgotPassword(false)} />
      ) : (
        <LoginForm
          onSuccess={onSuccess}
          onDismiss={onDismiss}
          onClickForgotPassword={() => setShowForgotPassword(true)}
        />
      )}
    </Modal>
  );
};

export default LoginModal;
