import { useKeyDown } from "common/hooks/useKeyDown";

import Toast, { toastTypes } from "components/Toast";

interface LoginSuccessToastProps {
  onClose: () => void;
}

const LoginSuccessToast = ({ onClose }: LoginSuccessToastProps) => {
  useKeyDown({ key: "Escape", onKeyDown: onClose });

  setTimeout(onClose, 3000);

  return (
    <>
      <div className="fixed right-4 bottom-4 z-10">
        <Toast
          message="Logged in as admin"
          onClose={onClose}
          type={toastTypes.success}
        />
      </div>
    </>
  );
};

export default LoginSuccessToast;
