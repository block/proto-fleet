import { ReactNode } from "react";
import clsx from "clsx";

import { cardType } from ".";

interface CardProps {
  children: ReactNode;
  title: string;
  type: (typeof cardType)[keyof typeof cardType];
}

const Card = ({ children, title, type }: CardProps) => {
  return (
    <div className="rounded-xl shadow-50">
      <div
        className={clsx("rounded-t-xl px-4 py-2", {
          "bg-core-primary-5 text-text-primary": type === cardType.default,
          "bg-intent-success-fill text-text-contrast": type === cardType.success,
          "bg-intent-critical-fill text-text-contrast": type === cardType.warning,
        })}
      >
        {title}
      </div>
      <div>{children}</div>
    </div>
  );
};

export default Card;
