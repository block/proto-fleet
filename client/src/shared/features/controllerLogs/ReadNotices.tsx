import { type CoverageNotice, formatControllerTime } from "./coverage";
import { Alert, Info } from "@/shared/assets/icons";
import Callout from "@/shared/components/Callout";

interface ReadNoticesProps {
  /** Title when a refresh fails while earlier data is still shown. */
  staleTitle: string;
  /** Title when nothing could be read. */
  unavailableTitle: string;
  /** When the data on screen was read; absent before the first successful read. */
  fetchedAt?: string;
  error?: string;
  coverage?: CoverageNotice;
  testIdPrefix: string;
}

/** Explains failed refreshes and partial coverage above a controller record view. */
const ReadNotices = ({ staleTitle, unavailableTitle, fetchedAt, error, coverage, testIdPrefix }: ReadNoticesProps) => (
  <>
    {error ? (
      <Callout
        className="mx-4 mb-3"
        intent={fetchedAt ? "warning" : "danger"}
        prefixIcon={<Alert />}
        title={fetchedAt ? staleTitle : unavailableTitle}
        subtitle={fetchedAt ? `Showing data read at ${formatControllerTime(fetchedAt)}. ${error}` : error}
        testId={`${testIdPrefix}-${fetchedAt ? "stale" : "unavailable"}`}
      />
    ) : null}
    {coverage ? (
      <Callout
        className="mx-4 mb-3"
        intent={coverage.intent}
        prefixIcon={coverage.intent === "information" ? <Info /> : <Alert />}
        title={coverage.title}
        subtitle={coverage.subtitle}
        testId={`${testIdPrefix}-coverage`}
      />
    ) : null}
  </>
);

export default ReadNotices;
