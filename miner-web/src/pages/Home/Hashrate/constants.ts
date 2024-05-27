import { deepClone, getRandomInt } from "common/utils/utility";

// real sample data from running the device
export const mockHashrateData = {
  duration: "12h",
  data: [
    {
      datetime: 1715897892,
      value: 74553316.0,
    },
    {
      datetime: 1715897955,
      value: 83108556.0,
    },
    {
      datetime: 1715898018,
      value: 82547586.0,
    },
    {
      datetime: 1715898078,
      value: 82306346.0,
    },
    {
      datetime: 1715898144,
      value: 81911512.0,
    },
    {
      datetime: 1715898205,
      value: 81474056.0,
    },
    {
      datetime: 1715898265,
      value: 81165832.0,
    },
    {
      datetime: 1715898326,
      value: 80908784.0,
    },
    {
      datetime: 1715898388,
      value: 80663300.0,
    },
    {
      datetime: 1715898448,
      value: 80387848.0,
    },
    {
      datetime: 1715898511,
      value: 80131792.0,
    },
    {
      datetime: 1715898574,
      value: 79914674.0,
    },
    {
      datetime: 1715898636,
      value: 79696304.0,
    },
    {
      datetime: 1715898698,
      value: 79518986.0,
    },
    {
      datetime: 1715898759,
      value: 79370882.0,
    },
    {
      datetime: 1715898821,
      value: 79225710.0,
    },
    {
      datetime: 1715898883,
      value: 79099986.0,
    },
    {
      datetime: 1715898946,
      value: 78955540.0,
    },
    {
      datetime: 1715899009,
      value: 78823248.0,
    },
    {
      datetime: 1715899071,
      value: 78697624.0,
    },
    {
      datetime: 1715899141,
      value: 78583216.0,
    },
    {
      datetime: 1715899203,
      value: 78462784.0,
    },
    {
      datetime: 1715899266,
      value: 78332110.0,
    },
    {
      datetime: 1715899326,
      value: 78221752.0,
    },
    {
      datetime: 1715899388,
      value: 78113028.0,
    },
    {
      datetime: 1715899450,
      value: 78016908.0,
    },
    {
      datetime: 1715899512,
      value: 77909984.0,
    },
    {
      datetime: 1715899573,
      value: 77808718.0,
    },
    {
      datetime: 1715899636,
      value: 77726656.0,
    },
    {
      datetime: 1715899697,
      value: 77625504.0,
    },
    {
      datetime: 1715899760,
      value: 77529320.0,
    },
    {
      datetime: 1715899822,
      value: 77439730.0,
    },
    {
      datetime: 1715899885,
      value: 77341608.0,
    },
    {
      datetime: 1715899947,
      value: 77265332.0,
    },
    {
      datetime: 1715900014,
      value: 77180210.0,
    },
    {
      datetime: 1715900077,
      value: 77101236.0,
    },
    {
      datetime: 1715900140,
      value: 77024622.0,
    },
    {
      datetime: 1715900205,
      value: 76942556.0,
    },
  ],
  aggregates: {
    min: 74553316.0,
    avg: 78975977.78947368,
    max: 83108556.0,
  },
};

export const mockHashrateData1 = {
  duration: "12h",
  data: [
    {
      datetime: 1715897887,
      value: 27409936.0,
    },
    {
      datetime: 1715897955,
      value: 28238262.0,
    },
    {
      datetime: 1715898017,
      value: 28214270.0,
    },
    {
      datetime: 1715898088,
      value: 28154014.0,
    },
    {
      datetime: 1715898154,
      value: 27989784.0,
    },
    {
      datetime: 1715898218,
      value: 27845744.0,
    },
    {
      datetime: 1715898281,
      value: 27816082.0,
    },
    {
      datetime: 1715898346,
      value: 27721224.0,
    },
    {
      datetime: 1715898408,
      value: 27670168.0,
    },
    {
      datetime: 1715898473,
      value: 27589602.0,
    },
    {
      datetime: 1715898537,
      value: 27497774.0,
    },
    {
      datetime: 1715898602,
      value: 27426810.0,
    },
    {
      datetime: 1715898665,
      value: 27389096.0,
    },
    {
      datetime: 1715898729,
      value: 27353298.0,
    },
    {
      datetime: 1715898800,
      value: 27297354.0,
    },
    {
      datetime: 1715898865,
      value: 27260688.0,
    },
    {
      datetime: 1715898931,
      value: 27236490.0,
    },
    {
      datetime: 1715898999,
      value: 27176896.0,
    },
    {
      datetime: 1715899060,
      value: 27141738.0,
    },
    {
      datetime: 1715899125,
      value: 27116156.0,
    },
    {
      datetime: 1715899191,
      value: 27086650.0,
    },
    {
      datetime: 1715899255,
      value: 27054958.0,
    },
    {
      datetime: 1715899319,
      value: 27021962.0,
    },
    {
      datetime: 1715899387,
      value: 26995574.0,
    },
    {
      datetime: 1715899450,
      value: 26961082.0,
    },
    {
      datetime: 1715899516,
      value: 26937118.0,
    },
    {
      datetime: 1715899581,
      value: 26908408.0,
    },
    {
      datetime: 1715899646,
      value: 26885966.0,
    },
    {
      datetime: 1715899708,
      value: 26845356.0,
    },
    {
      datetime: 1715899770,
      value: 26829110.0,
    },
    {
      datetime: 1715899832,
      value: 26805994.0,
    },
    {
      datetime: 1715899896,
      value: 26774690.0,
    },
    {
      datetime: 1715899957,
      value: 26752246.0,
    },
    {
      datetime: 1715900024,
      value: 26728496.0,
    },
    {
      datetime: 1715900087,
      value: 26707088.0,
    },
    {
      datetime: 1715900150,
      value: 26691558.0,
    },
    {
      datetime: 1715900215,
      value: 26670808.0,
    },
    {
      datetime: 1715900278,
      value: 26645658.0,
    },
  ],
  aggregates: {
    min: 26604400.0,
    avg: 27201920.65,
    max: 28238262.0,
  },
};

export const getMockHashrateData = (min: number, max: number) => {
  const hashrateData = deepClone(mockHashrateData1);
  hashrateData.data.map((data: { value: number }) => {
    data.value = data.value + getRandomInt(min, max) * 100000;
    return data;
  });

  return hashrateData;
};
