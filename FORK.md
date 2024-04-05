# Fork

## op-geth

- tag: `v1.101308.2`
- commit: `0402d543`
- differences
  - change predeployed contract address [#17](https://github.com/kroma-network/go-ethereum/pull/17).
  - remove daisy chain [#11](https://github.com/kroma-network/go-ethereum/pull/11).
  - enable tx pool sync by force [#13](https://github.com/kroma-network/go-ethereum/pull/13).
  - remove system tx related fields. (e.g, `IsSystemTx()`) [#29](https://github.com/kroma-network/go-ethereum/pull/29).
  - update ValidatorRewardScalar to L1Block contract [#36](https://github.com/kroma-network/go-ethereum/pull/36).
  - change transaction fee distribution logic [#36](https://github.com/kroma-network/go-ethereum/pull/36).
  - use zktrie instead of merkle patricia trie
  - use KromaZKTrie instead of scroll zktrie

## scroll-geth

- tag: `alpha-v1.9`
- differences: You can search differences by looking up 'This part is different from scroll' throughout the codes.
  - the base geth version is different.
  - maintain account{nonce, value, codehash, storageRoot} no touched.
  - add missing cases where needs to be set Zktrie flag to true.
  - the changes related websocket buffer size or compression rate are not applied.
  - rollup fee collection logic is slightly different. Ours inherits op-geth's one.
  - add `mint` property to `TransactionData`.
