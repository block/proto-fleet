export const createOrPredicate = <ItemType>(...predicates: Array<(value: ItemType) => boolean>) => {
  return (value: ItemType) => predicates.some((predicate) => predicate(value));
};

export const createAndPredicate = <ItemType>(...predicates: Array<(value: ItemType) => boolean>) => {
  return (value: ItemType) => predicates.every((predicate) => predicate(value));
};
