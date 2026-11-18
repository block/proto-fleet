import { type ReactNode, useState } from "react";
import ListPagination from "@/protoFleet/components/ListPagination";

const REPORT_PAGE_SIZE = 50;
export const ReportingRows = <T,>({ rows, children }: { rows: readonly T[]; children: (rows: T[]) => ReactNode }) => {
  const [page, setPage] = useState(0);
  const pages = Math.max(1, Math.ceil(rows.length / REPORT_PAGE_SIZE));
  const currentPage = Math.min(page, pages - 1);
  return (
    <>
      {children(rows.slice(currentPage * REPORT_PAGE_SIZE, (currentPage + 1) * REPORT_PAGE_SIZE))}
      {pages > 1 ? (
        <ListPagination
          firstItemIndex={currentPage * REPORT_PAGE_SIZE + 1}
          lastItemIndex={Math.min((currentPage + 1) * REPORT_PAGE_SIZE, rows.length)}
          total={rows.length}
          itemName="rows"
          hasPreviousPage={currentPage > 0}
          hasNextPage={currentPage < pages - 1}
          onPrevious={() => setPage(currentPage - 1)}
          onNext={() => setPage(currentPage + 1)}
        />
      ) : null}
    </>
  );
};
