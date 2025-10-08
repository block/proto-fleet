import Skeleton from ".";

export const SkeletonBar = () => {
  return (
    <>
      <Skeleton className="mb-4 h-8! w-96" />
      <Skeleton className="mb-4 h-8! w-72" />
      <Skeleton className="mb-4 h-8! w-80" />
    </>
  );
};

export default {
  title: "Shared/Loaders/Skeleton Bar",
};
