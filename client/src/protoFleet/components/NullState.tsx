import type { ReactNode } from "react";
import clsx from "clsx";

import Header from "@/shared/components/Header";

interface NullStateProps {
  icon?: ReactNode;
  title: string;
  description?: string;
  action?: ReactNode;
  className?: string;
  testId?: string;
}

const NullState = ({ icon, title, description, action, className, testId }: NullStateProps) => (
  <div className={clsx("flex h-full flex-col justify-center p-6 tablet:p-10", className)} data-testid={testId}>
    <div className="flex h-full w-full flex-col justify-center rounded-xl bg-core-primary-5 px-6 py-10 tablet:px-20 tablet:py-10">
      <div className="flex flex-col gap-6">
        <div className="flex flex-col gap-4">
          {icon ? (
            <div className="bg-core-surface-5 flex h-10 w-10 items-center justify-center rounded-lg">{icon}</div>
          ) : null}
          <Header title={title} titleSize="text-display-200" description={description} />
        </div>
        {action ? <div>{action}</div> : null}
      </div>
    </div>
  </div>
);

export default NullState;
