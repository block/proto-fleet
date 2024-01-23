import Skeleton from ".";

export const SkeletonBar = () => {
  return (
    <>
      <Skeleton className="!h-8 w-96 mb-4" />
      <Skeleton className="!h-8 w-72 mb-4" />
      <Skeleton className="!h-8 w-80 mb-4" />
    </>
  );
};

export default {
  component: SkeletonBar,
  title: "Skeleton Bar",
};
