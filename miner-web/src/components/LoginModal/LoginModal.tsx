import { useState } from "react";

import Modal from "components/Modal";

import ForgotPassword from "./ForgotPassword";
import LoginForm from "./LoginForm";

interface LoginModalProps {
  onContinue: () => void;
  onDismiss?: () => void;
}

const LoginModal = ({ onContinue, onDismiss }: LoginModalProps) => {
  const [showForgotPassword, setShowForgotPassword] = useState(false);

  return (
    <Modal onDismiss={onDismiss} showHeader={false} className="!w-[402px]">
      {showForgotPassword ? (
        <ForgotPassword onDismiss={() => setShowForgotPassword(false)} />
      ) : (
        <LoginForm
          onContinue={onContinue}
          onDismiss={onDismiss}
          onClickForgotPassword={() => setShowForgotPassword(true)}
        />
      )}
    </Modal>
  );
};

export default LoginModal;
