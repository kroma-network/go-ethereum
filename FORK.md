# Fork

## op-geth

- commit: `13ee9ab`
- differences
  - change predeployed contract address([17](https://github.com/wemixkanvas/go-ethereum/pull/17)).
  - remove daisy chain([11](https://github.com/wemixkanvas/go-ethereum/pull/11)).
  - enable tx pool sync by force([13](https://github.com/wemixkanvas/go-ethereum/pull/13)).
- todo
  - apply [regolith](https://github.com/ethereum-optimism/optimism/blob/develop/specs/network-upgrades.md#regolith) changes.

## scroll-geth

- tag: `alpha-v1.9`
- differences: You can search differences by looking up 'This part is different from scroll' throughout the codes.
  - the base geth version is different.
  - maintain account{nonce, value, codehash, storageRoot} no touched.
  - add missing cases where needs to be set Zktrie flag to true.
  - the changes related websocket buffer size or compression rate are not applied.
  - rollup fee collection logic is slightly different. Ours inherits op-geth's one.
  - add `mint` property to `TransactionData`.
