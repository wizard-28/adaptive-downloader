# Trace inventory — adaptive-S mechanism pivot

## Native Mahimahi delivery-opportunity traces (capacity schedules)

| family | duration s | mean KiB/s | min | p10 | median | p90 | max | CV | p90/p10 | frac<25% med | >=450 s |
|---|---|---|---|---|---|---|---|---|---|---|---|
| ATT-LTE-driving-2016 | 120.0 | 552.1 | 3000 | 88500 | 466500 | 1212000 | 3444000 | 0.86 | 13.7 | 0.12 | False |
| ATT-LTE-driving | 786.1 | 630.8 | 1500 | 156000 | 675000 | 1078500 | 1936500 | 0.56 | 6.9 | 0.10 | True |
| TMobile-LTE-driving | 474.6 | 1557.6 | 1500 | 478500 | 1242000 | 3127500 | 7432500 | 0.78 | 6.5 | 0.04 | True |
| TMobile-LTE-short | 140.0 | 2038.7 | 184500 | 670500 | 1735500 | 3663000 | 7389000 | 0.59 | 5.5 | 0.01 | False |
| TMobile-UMTS-driving | 928.4 | 246.4 | 0 | 18000 | 235500 | 493500 | 886500 | 0.78 | 27.4 | 0.18 | True |
| Verizon-EVDO-driving | 1062.0 | 63.5 | 0 | 6000 | 51000 | 145500 | 376500 | 0.89 | 24.2 | 0.16 | True |
| Verizon-LTE-driving | 1363.0 | 1185.3 | 0 | 205500 | 807000 | 2943000 | 5913000 | 0.93 | 14.3 | 0.10 | True |
| Verizon-LTE-short | 140.0 | 609.4 | 1500 | 163500 | 532500 | 1212000 | 2097000 | 0.64 | 7.4 | 0.06 | False |

## Throughput-trajectory traces (NOT capacity)

| name | samples s | mean KiB/s | min | median | max | CV | p90/p10 | note |
|---|---|---|---|---|---|---|---|---|
| 7Train1.csv | 4910 | 831.3 | 2500 | 659375 | 3925000 | 0.81 | 12.1 | throughput trajectory only (NOT capacity); ODbL 1.0 + DbCL 1.0 |
| 7Train2.csv | 3116 | 1106.7 | 2500 | 1052500 | 3175000 | 0.69 | 11.4 | throughput trajectory only (NOT capacity); ODbL 1.0 + DbCL 1.0 |
| Long_Island_Rail_Road.csv | 1447 | 524.0 | 0 | 385000 | 2062500 | 0.91 | 114.4 | throughput trajectory only (NOT capacity); ODbL 1.0 + DbCL 1.0 |
| QTrain.csv | 2404 | 1037.2 | 0 | 968750 | 3550000 | 0.75 | 44.5 | throughput trajectory only (NOT capacity); ODbL 1.0 + DbCL 1.0 |
| bus57.csv | 2403 | 2963.1 | 0 | 847500 | 124625000 | 4.36 | 4.8 | throughput trajectory only (NOT capacity); ODbL 1.0 + DbCL 1.0 |
| bus62.csv | 3293 | 3624.0 | 0 | 340000 | 124625000 | 4.46 | 2.9 | throughput trajectory only (NOT capacity); ODbL 1.0 + DbCL 1.0 |
| bus62_2.csv | 1452 | 3762.1 | 0 | 338125 | 120500000 | 4.13 | 3.2 | throughput trajectory only (NOT capacity); ODbL 1.0 + DbCL 1.0 |
| report_bicycle_0001.log | 531 | 3853.2 | 220002 | 3815293 | 7485440 | 0.40 | 3.5 | throughput trajectory only (NOT capacity); license not explicit (flagged in M2) |
| report_bicycle_0002.log | 658 | 3105.6 | 0 | 2842936 | 8863073 | 0.65 | 10.0 | throughput trajectory only (NOT capacity); license not explicit (flagged in M2) |
| report_bus_0001.log | 607 | 3369.3 | 432002 | 3355407 | 6998871 | 0.37 | 3.0 | throughput trajectory only (NOT capacity); license not explicit (flagged in M2) |
| report_bus_0002.log | 546 | 3751.1 | 0 | 4043316 | 8191900 | 0.44 | 4.2 | throughput trajectory only (NOT capacity); license not explicit (flagged in M2) |
| report_bus_0003.log | 758 | 2403.9 | 0 | 2454891 | 8017920 | 0.57 | 5.7 | throughput trajectory only (NOT capacity); license not explicit (flagged in M2) |
| report_bus_0004.log | 281 | 7266.2 | 0 | 6742696 | 13871324 | 0.46 | 3.3 | throughput trajectory only (NOT capacity); license not explicit (flagged in M2) |
| report_bus_0005.log | 338 | 4940.1 | 0 | 4878028 | 8355592 | 0.40 | 3.2 | throughput trajectory only (NOT capacity); license not explicit (flagged in M2) |
| report_bus_0006.log | 516 | 3963.6 | 343720 | 3519459 | 8671232 | 0.42 | 3.3 | throughput trajectory only (NOT capacity); license not explicit (flagged in M2) |
| report_car_0001.log | 468 | 4365.0 | 0 | 4142443 | 12879163 | 0.61 | 10.5 | throughput trajectory only (NOT capacity); license not explicit (flagged in M2) |
| report_car_0002.log | 566 | 3615.5 | 0 | 3557078 | 7747584 | 0.47 | 4.0 | throughput trajectory only (NOT capacity); license not explicit (flagged in M2) |
| report_train_0001.log | 506 | 2817.7 | 0 | 2747498 | 7569408 | 0.60 | 10.0 | throughput trajectory only (NOT capacity); license not explicit (flagged in M2) |
| report_train_0002.log | 496 | 2930.5 | 0 | 2696720 | 8239104 | 0.63 | 7.0 | throughput trajectory only (NOT capacity); license not explicit (flagged in M2) |
| report_train_0003.log | 532 | 2595.4 | 0 | 2533325 | 8890368 | 0.69 | 45.5 | throughput trajectory only (NOT capacity); license not explicit (flagged in M2) |
| report_tram_0001.log | 572 | 2640.4 | 21578 | 1790163 | 6565050 | 0.74 | 9.0 | throughput trajectory only (NOT capacity); license not explicit (flagged in M2) |
| report_tram_0002.log | 659 | 1715.5 | 0 | 1445153 | 7688192 | 0.82 | 35.1 | throughput trajectory only (NOT capacity); license not explicit (flagged in M2) |
| report_tram_0003.log | 472 | 4336.5 | 0 | 4511082 | 8959888 | 0.31 | 2.2 | throughput trajectory only (NOT capacity); license not explicit (flagged in M2) |

## Replay-model statement

- Mahimahi traces define a packet delivery schedule (capacity schedule), which mm-link replays;
  the trace file is repeated at EOF by default. Requests share this schedule.
- Throughput-trajectory traces record measured end-to-end throughput and are NOT path capacity;
  the M3 study showed an aggregate throughput envelope imposes the same ceiling on every
  concurrency level, so it cannot represent a concurrency/request-granularity problem.
