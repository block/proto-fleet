import type { ReactNode } from "react";
import "@/protoOS/components/ContentLayout/ListContentLayout.css";

export default function ModulesStoryLayout({ children }: { children: ReactNode }) {
  return (
    <div className="protoos-list-layout [container-type:inline-size] h-screen overflow-auto bg-surface-base text-text-primary">
      {children}
    </div>
  );
}
