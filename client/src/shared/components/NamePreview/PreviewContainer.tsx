import { type ReactNode } from "react";

interface PreviewContainerProps {
  children: ReactNode;
}

const PreviewContainer = ({ children }: PreviewContainerProps) => {
  return (
    <div className="flex min-h-40 items-center rounded-3xl bg-black/5 px-3 py-6">
      <div className="w-full overflow-x-auto pb-2 [scrollbar-gutter:stable]">
        <div className="mx-auto w-fit min-w-max text-center">{children}</div>
      </div>
    </div>
  );
};

export default PreviewContainer;
