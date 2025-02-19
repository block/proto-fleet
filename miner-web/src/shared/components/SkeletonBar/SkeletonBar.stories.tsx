import Skeleton from ".";

export const SkeletonBar = () => {
  return (
    <>
      <Skeleton className="h-8! w-96 mb-4" />
      <Skeleton className="h-8! w-72 mb-4" />
      <Skeleton className="h-8! w-80 mb-4" />
    </>
  );
};

export default {
  title: "Components (Shared)/Loaders/Skeleton Bar",
};
