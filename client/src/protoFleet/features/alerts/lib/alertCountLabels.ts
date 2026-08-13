// "5,000 miners" — the affected count a rollup row and its drill-in both report. A rule firing on something
// other than a miner counts "instances" instead, since its rows carry no device.
export const countLabel = (count: number, singular: string) =>
  `${count.toLocaleString()} ${count === 1 ? singular : `${singular}s`}`;
