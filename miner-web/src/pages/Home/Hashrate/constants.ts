import { deepClone, getRandomInt } from "common/utils/utility";

// real sample data from running the device
export const mockHashrateData = {
  "duration": "12h",
  "data": [
      {
          "datetime": 1718374655,
          "value": 53623084
      },
      {
          "datetime": 1718374716,
          "value": 60408474
      },
      {
          "datetime": 1718374776,
          "value": 61798808
      },
      {
          "datetime": 1718374836,
          "value": 62382220
      },
      {
          "datetime": 1718374896,
          "value": 62820504
      },
      {
          "datetime": 1718374956,
          "value": 62943278
      },
      {
          "datetime": 1718375016,
          "value": 63046082
      },
      {
          "datetime": 1718375077,
          "value": 63096780
      },
      {
          "datetime": 1718375138,
          "value": 63166750
      },
      {
          "datetime": 1718375198,
          "value": 63143454
      },
      {
          "datetime": 1718375259,
          "value": 63214142
      },
      {
          "datetime": 1718375320,
          "value": 63279196
      },
      {
          "datetime": 1718375381,
          "value": 63306906
      },
      {
          "datetime": 1718375441,
          "value": 63351156
      },
      {
          "datetime": 1718375501,
          "value": 63383202
      },
      {
          "datetime": 1718375561,
          "value": 63415824
      },
      {
          "datetime": 1718375621,
          "value": 63438294
      },
      {
          "datetime": 1718375681,
          "value": 63451320
      },
      {
          "datetime": 1718375741,
          "value": 63452718
      },
      {
          "datetime": 1718375802,
          "value": 63486280
      },
      {
          "datetime": 1718375862,
          "value": 63510322
      },
      {
          "datetime": 1718375922,
          "value": 63517848
      },
      {
          "datetime": 1718375982,
          "value": 63502896
      },
      {
          "datetime": 1718376043,
          "value": 63504294
      },
      {
          "datetime": 1718376103,
          "value": 63500882
      },
      {
          "datetime": 1718376163,
          "value": 63518840
      },
      {
          "datetime": 1718376224,
          "value": 63542056
      },
      {
          "datetime": 1718376285,
          "value": 63534446
      },
      {
          "datetime": 1718376345,
          "value": 63534230
      },
      {
          "datetime": 1718376405,
          "value": 63536336
      },
      {
          "datetime": 1718376465,
          "value": 63536188
      },
      {
          "datetime": 1718376526,
          "value": 63538662
      },
      {
          "datetime": 1718376587,
          "value": 63536820
      },
      {
          "datetime": 1718376648,
          "value": 63536866
      },
      {
          "datetime": 1718376708,
          "value": 63539112
      },
      {
          "datetime": 1718376768,
          "value": 63539778
      },
      {
          "datetime": 1718376830,
          "value": 63554820
      },
      {
          "datetime": 1718376890,
          "value": 63541216
      },
      {
          "datetime": 1718376950,
          "value": 63565840
      },
      {
          "datetime": 1718377012,
          "value": 63580838
      },
      {
          "datetime": 1718377073,
          "value": 63584330
      },
      {
          "datetime": 1718377134,
          "value": 63585602
      },
      {
          "datetime": 1718377194,
          "value": 63601262
      },
      {
          "datetime": 1718377254,
          "value": 63605170
      },
      {
          "datetime": 1718377315,
          "value": 63595940
      },
      {
          "datetime": 1718377375,
          "value": 63587848
      },
      {
          "datetime": 1718377435,
          "value": 63592516
      },
      {
          "datetime": 1718377495,
          "value": 63591422
      },
      {
          "datetime": 1718377557,
          "value": 63594672
      },
      {
          "datetime": 1718377618,
          "value": 63601756
      },
      {
          "datetime": 1718377678,
          "value": 63596378
      },
      {
          "datetime": 1718377740,
          "value": 63599488
      },
      {
          "datetime": 1718377800,
          "value": 63597948
      },
      {
          "datetime": 1718377861,
          "value": 63595702
      },
      {
          "datetime": 1718377921,
          "value": 63601046
      },
      {
          "datetime": 1718377982,
          "value": 63598526
      },
      {
          "datetime": 1718378042,
          "value": 63592734
      },
      {
          "datetime": 1718378102,
          "value": 63592554
      },
      {
          "datetime": 1718378163,
          "value": 63587064
      },
      {
          "datetime": 1718378224,
          "value": 63585538
      },
      {
          "datetime": 1718378284,
          "value": 63590864
      },
      {
          "datetime": 1718378347,
          "value": 63589942
      },
      {
          "datetime": 1718378407,
          "value": 63586268
      },
      {
          "datetime": 1718378469,
          "value": 63590716
      },
      {
          "datetime": 1718378531,
          "value": 63598816
      },
      {
          "datetime": 1718378594,
          "value": 63592390
      },
      {
          "datetime": 1718378654,
          "value": 63599084
      },
      {
          "datetime": 1718378714,
          "value": 63598864
      },
      {
          "datetime": 1718378774,
          "value": 63604782
      },
      {
          "datetime": 1718378835,
          "value": 63604300
      },
      {
          "datetime": 1718378895,
          "value": 63606608
      },
      {
          "datetime": 1718378955,
          "value": 63604674
      },
      {
          "datetime": 1718379015,
          "value": 63602490
      },
      {
          "datetime": 1718379075,
          "value": 63607988
      },
      {
          "datetime": 1718379136,
          "value": 63610324
      },
      {
          "datetime": 1718379196,
          "value": 63605918
      },
      {
          "datetime": 1718379256,
          "value": 63607202
      },
      {
          "datetime": 1718379316,
          "value": 63606184
      },
      {
          "datetime": 1718379376,
          "value": 63606114
      },
      {
          "datetime": 1718379436,
          "value": 63603974
      },
      {
          "datetime": 1718379497,
          "value": 63604094
      },
      {
          "datetime": 1718379557,
          "value": 63601406
      },
      {
          "datetime": 1718379617,
          "value": 63601516
      },
      {
          "datetime": 1718379678,
          "value": 63600062
      },
      {
          "datetime": 1718379739,
          "value": 63605478
      },
      {
          "datetime": 1718379800,
          "value": 63601474
      },
      {
          "datetime": 1718379862,
          "value": 63602886
      },
      {
          "datetime": 1718379924,
          "value": 63597890
      },
      {
          "datetime": 1718379985,
          "value": 63600774
      },
      {
          "datetime": 1718380046,
          "value": 63601620
      },
      {
          "datetime": 1718380108,
          "value": 63599944
      },
      {
          "datetime": 1718380168,
          "value": 63598830
      },
      {
          "datetime": 1718380228,
          "value": 63597098
      },
      {
          "datetime": 1718380289,
          "value": 63597250
      },
      {
          "datetime": 1718380349,
          "value": 63599986
      },
      {
          "datetime": 1718380409,
          "value": 63596996
      },
      {
          "datetime": 1718380470,
          "value": 63597560
      },
      {
          "datetime": 1718380530,
          "value": 63594084
      },
      {
          "datetime": 1718380591,
          "value": 63591288
      },
      {
          "datetime": 1718380652,
          "value": 63591742
      },
      {
          "datetime": 1718380713,
          "value": 63591476
      },
      {
          "datetime": 1718380773,
          "value": 63592596
      },
      {
          "datetime": 1718380834,
          "value": 63590414
      },
      {
          "datetime": 1718380896,
          "value": 63592392
      },
      {
          "datetime": 1718380957,
          "value": 63593838
      },
      {
          "datetime": 1718381018,
          "value": 63595980
      },
      {
          "datetime": 1718381079,
          "value": 63593474
      },
      {
          "datetime": 1718381139,
          "value": 63589626
      },
      {
          "datetime": 1718382816,
          "value": 52596496
      },
      {
          "datetime": 1718382876,
          "value": 59061396
      },
      {
          "datetime": 1718382937,
          "value": 60634344
      },
      {
          "datetime": 1718382999,
          "value": 61386760
      },
      {
          "datetime": 1718383060,
          "value": 61725188
      },
      {
          "datetime": 1718383120,
          "value": 62056678
      },
      {
          "datetime": 1718383180,
          "value": 62217788
      },
      {
          "datetime": 1718383241,
          "value": 62364562
      },
      {
          "datetime": 1718383303,
          "value": 62509190
      },
      {
          "datetime": 1718383365,
          "value": 62529098
      },
      {
          "datetime": 1718383425,
          "value": 62601208
      },
      {
          "datetime": 1718383487,
          "value": 62667726
      },
      {
          "datetime": 1718383548,
          "value": 62737042
      },
      {
          "datetime": 1718383608,
          "value": 62737044
      },
      {
          "datetime": 1718383670,
          "value": 62760630
      },
      {
          "datetime": 1718383731,
          "value": 62740616
      },
      {
          "datetime": 1718383792,
          "value": 62786838
      },
      {
          "datetime": 1718383854,
          "value": 62807670
      },
      {
          "datetime": 1718383916,
          "value": 62825884
      },
      {
          "datetime": 1718383978,
          "value": 62855766
      },
      {
          "datetime": 1718384039,
          "value": 62884476
      },
      {
          "datetime": 1718384100,
          "value": 62897838
      },
      {
          "datetime": 1718384162,
          "value": 62901708
      },
      {
          "datetime": 1718384224,
          "value": 62916598
      },
      {
          "datetime": 1718384284,
          "value": 62919788
      },
      {
          "datetime": 1718384346,
          "value": 62917716
      },
      {
          "datetime": 1718384408,
          "value": 62921988
      },
      {
          "datetime": 1718384469,
          "value": 62936888
      },
      {
          "datetime": 1718384530,
          "value": 62948612
      },
      {
          "datetime": 1718384592,
          "value": 62949162
      },
      {
          "datetime": 1718384654,
          "value": 62941740
      },
      {
          "datetime": 1718384715,
          "value": 62963670
      },
      {
          "datetime": 1718384776,
          "value": 62991350
      },
      {
          "datetime": 1718384838,
          "value": 63009604
      },
      {
          "datetime": 1718384900,
          "value": 63013524
      },
      {
          "datetime": 1718384961,
          "value": 63026794
      },
      {
          "datetime": 1718385024,
          "value": 63036146
      },
      {
          "datetime": 1718385091,
          "value": 62590806
      },
      {
          "datetime": 1718385151,
          "value": 62663888
      },
      {
          "datetime": 1718385212,
          "value": 62670836
      },
      {
          "datetime": 1718385272,
          "value": 62719358
      },
      {
          "datetime": 1718385333,
          "value": 62762904
      },
      {
          "datetime": 1718385393,
          "value": 62736818
      },
      {
          "datetime": 1718385454,
          "value": 62703436
      },
      {
          "datetime": 1718385515,
          "value": 62692976
      },
      {
          "datetime": 1718385575,
          "value": 62758854
      },
      {
          "datetime": 1718385635,
          "value": 62806836
      },
      {
          "datetime": 1718385695,
          "value": 62806294
      },
      {
          "datetime": 1718385756,
          "value": 62790632
      },
      {
          "datetime": 1718385816,
          "value": 62867924
      },
      {
          "datetime": 1718385876,
          "value": 62908632
      },
      {
          "datetime": 1718385936,
          "value": 62868210
      },
      {
          "datetime": 1718385996,
          "value": 62884026
      },
      {
          "datetime": 1718386057,
          "value": 62876306
      },
      {
          "datetime": 1718386117,
          "value": 62909350
      },
      {
          "datetime": 1718386179,
          "value": 62905178
      },
      {
          "datetime": 1718386240,
          "value": 62908396
      },
      {
          "datetime": 1718386301,
          "value": 62925982
      },
      {
          "datetime": 1718386363,
          "value": 62900890
      },
      {
          "datetime": 1718386425,
          "value": 62872820
      },
      {
          "datetime": 1718386486,
          "value": 62917104
      },
      {
          "datetime": 1718386549,
          "value": 62911658
      },
      {
          "datetime": 1718386610,
          "value": 62930526
      },
      {
          "datetime": 1718386670,
          "value": 62953626
      },
      {
          "datetime": 1718386731,
          "value": 62972676
      },
      {
          "datetime": 1718386793,
          "value": 62949838
      },
      {
          "datetime": 1718386854,
          "value": 62975166
      },
      {
          "datetime": 1718386916,
          "value": 62950144
      },
      {
          "datetime": 1718386978,
          "value": 62942758
      },
      {
          "datetime": 1718387040,
          "value": 62932804
      },
      {
          "datetime": 1718387102,
          "value": 62925408
      },
      {
          "datetime": 1718387163,
          "value": 62932096
      },
      {
          "datetime": 1718387224,
          "value": 62934574
      },
      {
          "datetime": 1718387284,
          "value": 62927708
      },
      {
          "datetime": 1718387344,
          "value": 62906394
      },
      {
          "datetime": 1718387404,
          "value": 62912116
      },
      {
          "datetime": 1718387464,
          "value": 62971838
      },
      {
          "datetime": 1718387524,
          "value": 62995740
      },
      {
          "datetime": 1718387584,
          "value": 63020222
      },
      {
          "datetime": 1718387645,
          "value": 63012172
      },
      {
          "datetime": 1718387707,
          "value": 63032222
      },
      {
          "datetime": 1718387769,
          "value": 63033624
      },
      {
          "datetime": 1718387831,
          "value": 63067912
      },
      {
          "datetime": 1718387891,
          "value": 63068902
      },
      {
          "datetime": 1718387953,
          "value": 63060336
      },
      {
          "datetime": 1718388015,
          "value": 63068460
      },
      {
          "datetime": 1718388076,
          "value": 63078894
      },
      {
          "datetime": 1718388138,
          "value": 63098928
      },
      {
          "datetime": 1718388200,
          "value": 63085592
      },
      {
          "datetime": 1718388260,
          "value": 63084284
      },
      {
          "datetime": 1718388320,
          "value": 63084706
      },
      {
          "datetime": 1718388380,
          "value": 63079586
      },
      {
          "datetime": 1718388441,
          "value": 63096966
      },
      {
          "datetime": 1718388501,
          "value": 63072748
      },
      {
          "datetime": 1718388562,
          "value": 63094990
      },
      {
          "datetime": 1718388623,
          "value": 63098810
      },
      {
          "datetime": 1718388684,
          "value": 63102764
      },
      {
          "datetime": 1718388744,
          "value": 63121022
      },
      {
          "datetime": 1718388804,
          "value": 63135876
      },
      {
          "datetime": 1718388865,
          "value": 63101686
      },
      {
          "datetime": 1718388927,
          "value": 63100898
      },
      {
          "datetime": 1718388988,
          "value": 63112382
      },
      {
          "datetime": 1718389048,
          "value": 63114042
      },
      {
          "datetime": 1718389108,
          "value": 63123470
      },
      {
          "datetime": 1718389168,
          "value": 63126078
      },
      {
          "datetime": 1718389228,
          "value": 63144294
      },
      {
          "datetime": 1718389289,
          "value": 63127126
      },
      {
          "datetime": 1718389349,
          "value": 63138180
      },
      {
          "datetime": 1718389409,
          "value": 63151432
      },
      {
          "datetime": 1718389470,
          "value": 63138840
      },
      {
          "datetime": 1718389530,
          "value": 63135288
      },
      {
          "datetime": 1718389591,
          "value": 63143772
      },
      {
          "datetime": 1718389654,
          "value": 63135716
      },
      {
          "datetime": 1718389715,
          "value": 63145030
      },
      {
          "datetime": 1718389776,
          "value": 63155416
      },
      {
          "datetime": 1718389838,
          "value": 63162628
      },
      {
          "datetime": 1718389899,
          "value": 63177040
      },
      {
          "datetime": 1718389961,
          "value": 63162480
      },
      {
          "datetime": 1718390022,
          "value": 63176180
      },
      {
          "datetime": 1718390084,
          "value": 63184532
      },
      {
          "datetime": 1718390146,
          "value": 63186428
      },
      {
          "datetime": 1718390208,
          "value": 63201386
      },
      {
          "datetime": 1718390269,
          "value": 63205644
      },
      {
          "datetime": 1718390330,
          "value": 63200530
      },
      {
          "datetime": 1718390393,
          "value": 63217168
      },
      {
          "datetime": 1718390453,
          "value": 63202648
      },
      {
          "datetime": 1718390514,
          "value": 63209436
      },
      {
          "datetime": 1718390575,
          "value": 63235584
      },
      {
          "datetime": 1718390636,
          "value": 63251394
      },
      {
          "datetime": 1718390698,
          "value": 63252868
      },
      {
          "datetime": 1718390760,
          "value": 63253104
      },
      {
          "datetime": 1718390820,
          "value": 63256844
      },
      {
          "datetime": 1718390881,
          "value": 63268446
      },
      {
          "datetime": 1718390943,
          "value": 63273348
      },
      {
          "datetime": 1718391005,
          "value": 63265774
      },
      {
          "datetime": 1718391068,
          "value": 63259670
      },
      {
          "datetime": 1718391130,
          "value": 63252584
      },
      {
          "datetime": 1718391190,
          "value": 63252350
      },
      {
          "datetime": 1718391251,
          "value": 63256496
      },
      {
          "datetime": 1718391313,
          "value": 63265144
      },
      {
          "datetime": 1718391373,
          "value": 63255946
      },
      {
          "datetime": 1718391435,
          "value": 63266742
      },
      {
          "datetime": 1718391496,
          "value": 63257476
      },
      {
          "datetime": 1718391556,
          "value": 63264348
      },
      {
          "datetime": 1718391616,
          "value": 63283652
      }
  ],
  "aggregates": {
      "min": 52596496,
      "avg": 63080676.3715415,
      "max": 63610324
  }
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
