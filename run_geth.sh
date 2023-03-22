#!/bin/sh
set -exu

ZK=${ZK:-0}
if [ $ZK == 1 ]; then
	echo "ZK"
	GETH_DATA_DIR=./data_zk
	GENESIS_FILE_PATH="${GENESIS_FILE_PATH:-./genesis_zktrie.json}"
else
	echo "NON-ZK"
	GETH_DATA_DIR=./data
	GENESIS_FILE_PATH="${GENESIS_FILE_PATH:-./genesis.json}"
fi

VERBOSITY=${GETH_VERBOSITY:-3}
GETH_CHAINDATA_DIR="$GETH_DATA_DIR/geth/chaindata"
GETH_KEYSTORE_DIR="$GETH_DATA_DIR/keystore"
CHAIN_ID=$(cat "$GENESIS_FILE_PATH" | jq -r .config.chainId)
BLOCK_SIGNER_PRIVATE_KEY="0xec4d17b92a785136c39472014d7c7f5cde6ef5b56cbab8d10fd4d8e7e3e5f96c"
BLOCK_SIGNER_ADDRESS="0xBaDa1d8e766dbC7c3043007a7186F57c0678E28f"
RPC_PORT="${RPC_PORT:-8545}"
WS_PORT="${WS_PORT:-8546}"

mkdir -p $GETH_DATA_DIR

if [ ! -d "$GETH_KEYSTORE_DIR" ]; then
	echo "$GETH_KEYSTORE_DIR missing, running account import"
	echo "pwd" > "$GETH_DATA_DIR"/password
	echo "$BLOCK_SIGNER_PRIVATE_KEY" | sed 's/0x//' > "$GETH_DATA_DIR"/block-signer-key
	build/bin/geth account import \
		--datadir="$GETH_DATA_DIR" \
		--password="$GETH_DATA_DIR"/password \
		"$GETH_DATA_DIR"/block-signer-key
else
	echo "$GETH_KEYSTORE_DIR exists."
fi

if [ ! -d "$GETH_CHAINDATA_DIR" ]; then
	echo "$GETH_CHAINDATA_DIR missing, running init"
	echo "Initializing genesis."
	build/bin/geth --verbosity="$VERBOSITY" init \
		--datadir="$GETH_DATA_DIR" \
		"$GENESIS_FILE_PATH"
else
	echo "$GETH_CHAINDATA_DIR exists."
fi

# Warning: Archive mode is required, otherwise old trie nodes will be
# pruned within minutes of starting the devnet.

exec build/bin/geth \
	--datadir="$GETH_DATA_DIR" \
	--verbosity="$VERBOSITY" \
	--http \
	--http.corsdomain="*" \
	--http.vhosts="*" \
	--http.addr=0.0.0.0 \
	--http.port="$RPC_PORT" \
	--http.api=web3,debug,eth,txpool,net,engine,kanvas \
	--ws \
	--ws.addr=0.0.0.0 \
	--ws.port="$WS_PORT" \
	--ws.origins="*" \
	--ws.api=debug,eth,txpool,net,engine,kanvas \
	--syncmode=full \
	--nodiscover \
	--maxpeers=1 \
	--networkid=$CHAIN_ID \
	--unlock=$BLOCK_SIGNER_ADDRESS \
	--mine \
	--miner.etherbase=$BLOCK_SIGNER_ADDRESS \
	--password="$GETH_DATA_DIR"/password \
	--allow-insecure-unlock \
	--trace.mptwitness=1 \
	"$@"
