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
          "text-text-primary bg-surface-5": type === cardType.default,
          "text-intent-success-text bg-intent-success-fill/20": type === cardType.success,
          "text-intent-critical-text bg-intent-critical-fill/20": type === cardType.warning,
        })}
      >
        {title}
      </div>
      <div className="px-4">
        {children}
      </div>
    </div>
  );
};

export default Card;
