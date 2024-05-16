import { deepClone } from "common/utils/utility";

// real sample data from running the device
export const mockHashrateData = {
  duration: "12h",
  data: [
    {
      datetime: 1715617336,
      value: 4069070.0,
    },
    {
      datetime: 1715617396,
      value: 4115689.0,
    },
    {
      datetime: 1715617456,
      value: 3822781.0,
    },
    {
      datetime: 1715617517,
      value: 3595850.0,
    },
    {
      datetime: 1715617579,
      value: 3477513.0,
    },
    {
      datetime: 1715617639,
      value: 3394557.0,
    },
    {
      datetime: 1715617700,
      value: 3337448.0,
    },
    {
      datetime: 1715617760,
      value: 3168758.0,
    },
    {
      datetime: 1715617821,
      value: 3031614.0,
    },
    {
      datetime: 1715617882,
      value: 2918484.0,
    },
    {
      datetime: 1715617943,
      value: 2833910.0,
    },
    {
      datetime: 1715618003,
      value: 2765562.0,
    },
    {
      datetime: 1715618063,
      value: 2701026.0,
    },
    {
      datetime: 1715618124,
      value: 2650617.0,
    },
    {
      datetime: 1715618185,
      value: 2602694.0,
    },
    {
      datetime: 1715618246,
      value: 2562462.0,
    },
    {
      datetime: 1715618306,
      value: 2532888.0,
    },
    {
      datetime: 1715618366,
      value: 2504540.0,
    },
    {
      datetime: 1715618427,
      value: 2475293.0,
    },
    {
      datetime: 1715618487,
      value: 2450168.0,
    },
    {
      datetime: 1715618548,
      value: 2429956.0,
    },
    {
      datetime: 1715618608,
      value: 2411758.0,
    },
    {
      datetime: 1715618669,
      value: 2394426.0,
    },
    {
      datetime: 1715618730,
      value: 2374137.0,
    },
    {
      datetime: 1715618791,
      value: 2357254.0,
    },
    {
      datetime: 1715618851,
      value: 2338257.0,
    },
    {
      datetime: 1715618912,
      value: 2321383.0,
    },
    {
      datetime: 1715618973,
      value: 2307980.0,
    },
    {
      datetime: 1715619034,
      value: 2295586.0,
    },
    {
      datetime: 1715619095,
      value: 2284246.0,
    },
    {
      datetime: 1715619156,
      value: 2253267.0,
    },
    {
      datetime: 1715619216,
      value: 2212421.0,
    },
    {
      datetime: 1715619277,
      value: 2172511.0,
    },
    {
      datetime: 1715619338,
      value: 2136264.0,
    },
    {
      datetime: 1715619398,
      value: 2104304.0,
    },
    {
      datetime: 1715619459,
      value: 2072528.0,
    },
    {
      datetime: 1715619520,
      value: 2042493.0,
    },
    {
      datetime: 1715619580,
      value: 2013884.0,
    },
    {
      datetime: 1715619641,
      value: 1987591.0,
    },
    {
      datetime: 1715619702,
      value: 1962163.0,
    },
    {
      datetime: 1715619763,
      value: 1938096.0,
    },
    {
      datetime: 1715619824,
      value: 1915137.0,
    },
    {
      datetime: 1715619885,
      value: 1892874.0,
    },
    {
      datetime: 1715619946,
      value: 1872768.0,
    },
    {
      datetime: 1715620008,
      value: 1852123.0,
    },
    {
      datetime: 1715620069,
      value: 1832722.0,
    },
    {
      datetime: 1715620129,
      value: 1814901.0,
    },
    {
      datetime: 1715620191,
      value: 1797704.0,
    },
    {
      datetime: 1715620253,
      value: 1781166.0,
    },
    {
      datetime: 1715620313,
      value: 1764516.0,
    },
    {
      datetime: 1715620374,
      value: 1748495.0,
    },
    {
      datetime: 1715620437,
      value: 1732436.0,
    },
    {
      datetime: 1715620499,
      value: 1718907.0,
    },
    {
      datetime: 1715620559,
      value: 1705803.0,
    },
    {
      datetime: 1715620620,
      value: 1693354.0,
    },
    {
      datetime: 1715620682,
      value: 1679844.0,
    },
    {
      datetime: 1715620744,
      value: 1667471.0,
    },
    {
      datetime: 1715620805,
      value: 1656211.0,
    },
    {
      datetime: 1715620866,
      value: 1644137.0,
    },
    {
      datetime: 1715620926,
      value: 1632695.0,
    },
    {
      datetime: 1715620989,
      value: 1621674.0,
    },
  ],
  aggregates: {
    min: 1621674.0,
    avg: 2335186.344262295,
    max: 4115689.0,
  },
};

export const getMockHashrateData = () => {
  const hashrateData = deepClone(mockHashrateData);
  hashrateData.data.map((data: { value: number }) => {
    data.value = data.value + Math.floor(Math.random() * 100000);
    return data;
  });

  return hashrateData;
};
