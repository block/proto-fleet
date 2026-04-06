import { useActivity } from "@/protoFleet/api/useActivity";
import { useExportActivity } from "@/protoFleet/api/useExportActivity";
import ActivityTable from "@/protoFleet/features/activity/components/ActivityTable";
import { Alert } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Header from "@/shared/components/Header";
import ProgressCircular from "@/shared/components/ProgressCircular";

const PAGE_SIZE = 50;

const ActivityPage = () => {
  const { activities, totalCount, isLoading, error, hasMore, loadMore } = useActivity({
    pageSize: PAGE_SIZE,
  });
  const { exportCsv, isExportingCsv } = useExportActivity();

  const isInitialLoad = isLoading && activities.length === 0;
  const isLoadingMore = isLoading && activities.length > 0;

  if (isInitialLoad) {
    return (
      <div className="flex h-full items-center justify-center">
        <ProgressCircular indeterminate />
      </div>
    );
  }

  return (
    <>
      <div className="sticky left-0 z-3 px-10 pt-10 phone:px-6 phone:pt-6 tablet:px-6 tablet:pt-6">
        <div className="flex items-center justify-between pb-6">
          <Header title="Activity" titleSize="text-heading-300" />
          <Button
            variant={variants.secondary}
            size={sizes.compact}
            onClick={() => exportCsv()}
            loading={isExportingCsv}
            disabled={isExportingCsv || totalCount === 0}
          >
            Export CSV
          </Button>
        </div>
      </div>

      {error ? (
        <Callout className="mx-10 mb-4 phone:mx-6 tablet:mx-6" intent="danger" prefixIcon={<Alert />} title={error} />
      ) : null}

      <div className="p-10 pt-0 phone:p-6 phone:pt-0 tablet:p-6 tablet:pt-0">
        <ActivityTable activities={activities} totalCount={totalCount} />
        {hasMore && (
          <div className="flex justify-center py-6">
            <Button
              variant={variants.secondary}
              size={sizes.compact}
              onClick={loadMore}
              loading={isLoadingMore}
              disabled={isLoadingMore}
            >
              Load more
            </Button>
          </div>
        )}
      </div>
    </>
  );
};

export default ActivityPage;
