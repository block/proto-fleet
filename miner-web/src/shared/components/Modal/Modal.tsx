import { ReactNode, useCallback, useEffect, useRef, useState } from "react";
import clsx from "clsx";

import { Dismiss } from "@/shared/assets/icons";
import { sizes, variants } from "@/shared/components/Button";

import { ButtonProps } from "@/shared/components/ButtonGroup";
import Divider from "@/shared/components/Divider";
import Header from "@/shared/components/Header";
import PageOverlay, {
  animationDuration,
} from "@/shared/components/PageOverlay";
import { useClickOutside } from "@/shared/hooks/useClickOutside";
import { useKeyDown } from "@/shared/hooks/useKeyDown";

// optional prop to delay close modal on clicking button and allow animations to finish
interface ModalButtonProps extends ButtonProps {
  dismissModalOnClick?: boolean;
}

interface ModalProps {
  children: ReactNode;
  className?: string;
  contentHeader?: string;
  onDismiss?: (buttonClicked?: boolean) => void;
  buttonSize?: keyof typeof sizes;
  buttons?: ModalButtonProps[];
  show?: boolean;
  showHeader?: boolean;
  scrolledHeader?: boolean;
  title?: string;
  description?: string;
  preventClose?: boolean;
}

const Modal = ({
  children,
  className,
  contentHeader,
  onDismiss,
  buttonSize,
  buttons,
  show = true,
  showHeader = true,
  scrolledHeader = true,
  title,
  description,
  preventClose,
}: ModalProps) => {
  const [showModal, setShowModal] = useState(show);
  const ModalRef = useRef<HTMLDivElement>(null);

  const closeModal = useCallback(
    (buttonClicked?: boolean) => {
      if (onDismiss === undefined) {
        return;
      }
      setShowModal(false);
      setTimeout(() => {
        onDismiss(buttonClicked);
      }, animationDuration);
    },
    [onDismiss],
  );

  useEffect(() => {
    if (!show) {
      closeModal();
    }
  }, [closeModal, show]);

  // if button is supposed to dismiss modal, animate closing it
  const onButtonClick = useCallback(
    (button?: ModalButtonProps) => () => {
      if (
        button?.variant === variants.primary &&
        !button?.dismissModalOnClick
      ) {
        closeModal(true);
      }
      button?.onClick?.();
    },
    [closeModal],
  );

  const dismissModal = useCallback(() => {
    closeModal();
  }, [closeModal]);

  useKeyDown({ key: "Escape", onKeyDown: dismissModal });

  useClickOutside({ ref: ModalRef, onClickOutside: dismissModal });

  return (
    <PageOverlay show={showModal}>
      <div
        className={clsx(
          "h-fit w-[640px] rounded-3xl bg-surface-elevated-base p-6 shadow-300",
          {
            "animate-sliding-up": showModal,
            "animate-sliding-down": !showModal,
          },
          className,
        )}
        ref={ModalRef}
        data-testid="modal"
      >
        {showHeader && (
          <>
            <Header
              title={title}
              description={description}
              titleSize="text-heading-200"
              icon={preventClose ? undefined : <Dismiss />}
              iconOnClick={preventClose ? undefined : dismissModal}
              buttonSize={buttonSize}
              buttons={buttons?.map((button) => ({
                ...button,
                onClick: onButtonClick(button),
              }))}
              inline
            />
            {scrolledHeader ? (
              !preventClose && <Divider className="mt-6" />
            ) : (
              <div className="mt-6" />
            )}
          </>
        )}
        {contentHeader && (
          <div className="mb-1 text-heading-200 text-text-primary">
            {contentHeader}
          </div>
        )}
        <div className="text-300 text-text-primary-70">{children}</div>
      </div>
    </PageOverlay>
  );
};

export default Modal;
