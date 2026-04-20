type Cleanup = () => void;

export const createRefCountedStoryMock = (installMock: () => Cleanup) => {
  let activeInstances = 0;
  let cleanup: Cleanup | null = null;

  return () => {
    if (activeInstances === 0) {
      cleanup = installMock();
    }

    activeInstances += 1;

    return () => {
      activeInstances -= 1;

      if (activeInstances === 0) {
        cleanup?.();
        cleanup = null;
      }
    };
  };
};
