import { PAGE_GUTTERS } from "./spacing";
import type { ContentLayoutProps } from "./types";

export default function ListContentInset({ children }: ContentLayoutProps) {
  return <div className={`sticky left-0 w-[100cqw] ${PAGE_GUTTERS}`}>{children}</div>;
}
