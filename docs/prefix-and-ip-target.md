## `WARM_PREFIX_TARGET`, `WARM_IP_TARGET` and `MINIMUM_IP_TARGET`

IPAMD will start allocating (/28) prefixes to the ENIs with `ENABLE_PREFIX_DELEGATION` set to `true`. By default IPAMD will allocate 1 prefix for the allocated ENI but based on the need the number of prefixes to be held in warm pool can be controlled by setting `WARM_PREFIX_TARGET`, `WARM_IP_TARGET` and `MINIMUM_IP_TARGET` environment variables. 

`WARM_IP_TARGET` and `MINIMUM_IP_TARGET` if set will override `WARM_PREFIX_TARGET`. `WARM_PREFIX_TARGET` will allocate one full (/28) prefix even if a single IP is consumed with the existing prefix. If the ENI has no space to allocate a prefix then a new ENI will be created. So make sure to use this on need basis i.e, if pod density is high since this will be carved out of the ENIs subnet. `WARM_IP_TARGET` and `MINIUM_IP_TARGET` give more fine grained control on the number of IPs but if existing prefixes are not sufficient to maintain the warm pool then IPAMD will allocate more prefixes to the existing ENI or create a new ENI if the existing ENIs are running out of prefixes.

When a new ENI is allocated, IPAMD will allocate either 1 prefix or number of prefixes needed to maintain the `WARM_PREFIX_TARGET`, `WARM_IP_TARGET` and `MINIMUM_IP_TARGET` setting. This is done to avoid extra EC2 calls to either allocate more prefixes or free extra prefixes on ENI bring up.


Some example cases:

| Instance type | `WARM_PREFIX_TARGET`| `WARM_IP_TARGET`| `MINIMUM_IP_TARGET` | Pods | ENIs | Pod per ENIs | Attached Prefixes | Unused Prefixes | Prefixes per ENI | Unused IPs|
|---------------|:-------------------:|:---------------:|:-------------------:|:----:|:----:|:------------:|:-----------------:|:---------------:|:----------------:|:---------:|
| t3.small      |         1           |         -       |          -          |   0  |  1   |      0       |        1          |      1          |      1           |    16     |
| t3.small      |         1           |         -       |          -          |   5  |  3   |    1,2,2     |        4          |      1          |    2,1,1         |    59     |
| t3.small      |         1           |         -       |          -          |  17  |  1   |     17       |        3          |      1          |      3           |    31     |
|               |                     |                 |                     |      |      |              |                   |                 |                  |           |
| t3.small      |         -           |         1       |          1          |   0  |  1   |      0       |        1          |      1          |      1           |    16     |
| t3.small      |         -           |         1       |          1          |   5  |  3   |    1,2,2     |        3          |      0          |    1,1,1         |    43     |
| t3.small      |         -           |         1       |          1          |  17  |  1   |     17       |        2          |      0          |      2           |    15     |
|               |                     |                 |                     |      |      |              |                   |                 |                  |           |
| t3.small      |         -           |         2       |         10          |   0  |  1   |      0       |        1          |      1          |      1           |    16     |
| t3.small      |         -           |         2       |         10          |   5  |  3   |    1,2,2     |        3          |      0          |    1,1,1         |    43     |
| t3.small      |         -           |         2       |         10          |  17  |  1   |     17       |        2          |      0          |      2           |    15     |
|               |                     |                 |                     |      |      |              |                   |                 |                  |           |
| p3dn.24xlarge |         1           |         -       |          -          |   0  |  1   |      0       |        1          |      1          |      1           |    16     |
| p3dn.24xlarge |         1           |         -       |          -          |   3  |  2   |    3,0       |        2          |      1          |    2,0           |    29     |
| p3dn.24xlarge |         1           |         -       |          -          |  95  |  3   |   95,0,0     |        7          |      1          |    7,0,0         |    17     |
|               |                     |                 |                     |      |      |              |                   |                 |                  |           |
| p3dn.24xlarge |         -           |         5       |         10          |   0  |  1   |      0       |        1          |      1          |      1           |    16     |
| p3dn.24xlarge |         -           |         5       |         10          |   7  |  1   |      7       |        1          |      0          |      1           |     9     |
| p3dn.24xlarge |         -           |         5       |         10          |  15  |  1   |     15       |        2          |      1          |      2           |    17     |
| p3dn.24xlarge |         -           |         5       |         10          |  45  |  2   |   45,0       |        4          |      1          |    4,0           |    19     |
|               |                     |                 |                     |      |      |              |                   |                 |                  |           |