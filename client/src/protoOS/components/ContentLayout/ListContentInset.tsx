import type { ContentLayoutProps } from "./types";

export default function ListContentInset({ children }: ContentLayoutProps) {
  return <div className="sticky left-0 w-[100cqw] px-6 tablet:px-10 laptop:px-14">{children}</div>;
}
