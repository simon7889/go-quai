// Copyright 2014 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package eth

import (
	"errors"
	"fmt"
	"io"
	"math/big"

	"github.com/dominant-strategies/go-quai/common"
	"github.com/dominant-strategies/go-quai/core/forkid"
	"github.com/dominant-strategies/go-quai/core/types"
	"github.com/dominant-strategies/go-quai/rlp"
)

// Constants to match up protocol versions and messages
const (
	ETH65 = 65
	ETH66 = 66
)

// ProtocolName is the official short name of the `quai` protocol used during
// devp2p capability negotiation.
const c_ProtocolName = "quai"

// ProtocolVersions are the supported versions of the `eth` protocol (first
// is primary).
var ProtocolVersions = []uint{ETH66, ETH65}

// protocolLengths are the number of implemented message corresponding to
// different protocol versions.
var protocolLengths = map[uint]uint64{ETH66: 21, ETH65: 19}

// maxMessageSize is the maximum cap on the size of a protocol message.
const maxMessageSize = 10 * 1024 * 1024

const (
	// Protocol messages in eth/64
	StatusMsg          = 0x00
	NewBlockHashesMsg  = 0x01
	TransactionsMsg    = 0x02
	GetBlockHeadersMsg = 0x03
	BlockHeadersMsg    = 0x04
	GetBlockBodiesMsg  = 0x05
	BlockBodiesMsg     = 0x06
	NewBlockMsg        = 0x07
	GetNodeDataMsg     = 0x0d
	NodeDataMsg        = 0x0e
	GetReceiptsMsg     = 0x0f
	ReceiptsMsg        = 0x10

	// Protocol messages overloaded in eth/65
	NewPooledTransactionHashesMsg = 0x08
	GetPooledTransactionsMsg      = 0x09
	PooledTransactionsMsg         = 0x0a

	GetBlockMsg = 0x0b

	PendingEtxsMsg             = 0x11
	GetOnePendingEtxsMsg       = 0x12
	PendingEtxsRollupMsg       = 0x13
	GetOnePendingEtxsRollupMsg = 0x14
)

var (
	errNoStatusMsg             = errors.New("no status message")
	errMsgTooLarge             = errors.New("message too long")
	errDecode                  = errors.New("invalid message")
	errInvalidMsgCode          = errors.New("invalid message code")
	errProtocolVersionMismatch = errors.New("protocol version mismatch")
	errNetworkIDMismatch       = errors.New("network ID mismatch")
	errGenesisMismatch         = errors.New("genesis mismatch")
	errForkIDRejected          = errors.New("fork ID rejected")
	errLocationMismatch        = errors.New("location mismatch")
)

// Packet represents a p2p message in the `eth` protocol.
type Packet interface {
	Name() string // Name returns a string corresponding to the message type.
	Kind() byte   // Kind returns the message type.
}

// StatusPacket is the network packet for the status message for eth/64 and later.
type StatusPacket struct {
	ProtocolVersion uint32
	NetworkID       uint64
	Location        string
	Entropy         *big.Int
	Head            common.Hash
	Genesis         common.Hash
	ForkID          forkid.ID
}

// NewBlockHashesPacket is the network packet for the block announcements.
type NewBlockHashesPacket []struct {
	Hash   common.Hash // Hash of one particular block being announced
	Number uint64      // Number of one particular block being announced
}

// Unpack retrieves the block hashes and numbers from the announcement packet
// and returns them in a split flat format that's more consistent with the
// internal data structures.
func (p *NewBlockHashesPacket) Unpack() ([]common.Hash, []uint64) {
	var (
		hashes  = make([]common.Hash, len(*p))
		numbers = make([]uint64, len(*p))
	)
	for i, body := range *p {
		hashes[i], numbers[i] = body.Hash, body.Number
	}
	return hashes, numbers
}

// TransactionsPacket is the network packet for broadcasting new transactions.
type TransactionsPacket []*types.Transaction

// GetBlockHeadersPacket represents a block header query.
type GetBlockHeadersPacket struct {
	Origin  HashOrNumber // Block from which to retrieve headers
	Amount  uint64       // Maximum number of headers to retrieve
	Dom     bool         // true: Return only dom blocks upto amount, False : Return only non-dom blocks upto amount or dom block
	Reverse bool         // Query direction (false = rising towards latest, true = falling towards genesis)
	To      uint64       // Stop the fetch when To number is reached
	Skip    uint64       // The number of headers to skip between fetching the header from local database.
}

// GetBlockHeadersPacket represents a block header query over eth/66
type GetBlockHeadersPacket66 struct {
	RequestId uint64
	*GetBlockHeadersPacket
}

// HashOrNumber is a combined field for specifying an origin block.
type HashOrNumber struct {
	Hash   common.Hash // Block hash from which to retrieve headers (excludes Number)
	Number uint64      // Block hash from which to retrieve headers (excludes Hash)
}

// EncodeRLP is a specialized encoder for HashOrNumber to encode only one of the
// two contained union fields.
func (hn *HashOrNumber) EncodeRLP(w io.Writer) error {
	if hn.Hash == (common.Hash{}) {
		return rlp.Encode(w, hn.Number)
	}
	if hn.Number != 0 {
		return fmt.Errorf("both origin hash (%x) and number (%d) provided", hn.Hash, hn.Number)
	}
	return rlp.Encode(w, hn.Hash)
}

// DecodeRLP is a specialized decoder for HashOrNumber to decode the contents
// into either a block hash or a block number.
func (hn *HashOrNumber) DecodeRLP(s *rlp.Stream) error {
	_, size, err := s.Kind()
	switch {
	case err != nil:
		return err
	case size == 32:
		hn.Number = 0
		return s.Decode(&hn.Hash)
	case size <= 8:
		hn.Hash = common.Hash{}
		return s.Decode(&hn.Number)
	default:
		return fmt.Errorf("invalid input size %d for origin", size)
	}
}

// BlockHeadersPacket represents a block header response.
type BlockHeadersPacket []*types.Header

// BlockHeadersPacket represents a block header response over eth/66.
type BlockHeadersPacket66 struct {
	RequestId uint64
	BlockHeadersPacket
}

// NewBlockPacket is the network packet for the block propagation message.
type NewBlockPacket struct {
	Block *types.Block
}

// sanityCheck verifies that the values are reasonable, as a DoS protection
func (request *NewBlockPacket) sanityCheck() error {
	if err := request.Block.SanityCheck(); err != nil {
		return err
	}
	return nil
}

// GetBlockBodiesPacket represents a block body query.
type GetBlockBodiesPacket []common.Hash

// GetBlockBodiesPacket represents a block body query over eth/66.
type GetBlockBodiesPacket66 struct {
	RequestId uint64
	GetBlockBodiesPacket
}

// BlockBodiesPacket is the network packet for block content distribution.
type BlockBodiesPacket []*BlockBody

// BlockBodiesPacket is the network packet for block content distribution over eth/66.
type BlockBodiesPacket66 struct {
	RequestId uint64
	BlockBodiesPacket
}

// BlockBodiesRLPPacket is used for replying to block body requests, in cases
// where we already have them RLP-encoded, and thus can avoid the decode-encode
// roundtrip.
type BlockBodiesRLPPacket []rlp.RawValue

// BlockBodiesRLPPacket66 is the BlockBodiesRLPPacket over eth/66
type BlockBodiesRLPPacket66 struct {
	RequestId uint64
	BlockBodiesRLPPacket
}

// BlockBody represents the data content of a single block.
type BlockBody struct {
	Transactions    []*types.Transaction // Transactions contained within a block
	Uncles          []*types.Header      // Uncles contained within a block
	ExtTransactions []*types.Transaction
	SubManifest     types.BlockManifest
}

// Unpack retrieves the transactions and uncles from the range packet and returns
// them in a split flat format that's more consistent with the internal data structures.
func (p *BlockBodiesPacket) Unpack() ([][]*types.Transaction, [][]*types.Header, [][]*types.Transaction, []types.BlockManifest) {
	var (
		txset    = make([][]*types.Transaction, len(*p))
		uncleset = make([][]*types.Header, len(*p))
		etxset   = make([][]*types.Transaction, len(*p))
		manifest = make([]types.BlockManifest, len(*p))
	)
	for i, body := range *p {
		txset[i], uncleset[i], etxset[i], manifest[i] = body.Transactions, body.Uncles, body.ExtTransactions, body.SubManifest
	}
	return txset, uncleset, etxset, manifest
}

// GetNodeDataPacket represents a trie node data query.
type GetNodeDataPacket []common.Hash

// GetNodeDataPacket represents a trie node data query over eth/66.
type GetNodeDataPacket66 struct {
	RequestId uint64
	GetNodeDataPacket
}

// NodeDataPacket is the network packet for trie node data distribution.
type NodeDataPacket [][]byte

// NodeDataPacket is the network packet for trie node data distribution over eth/66.
type NodeDataPacket66 struct {
	RequestId uint64
	NodeDataPacket
}

// GetReceiptsPacket represents a block receipts query.
type GetReceiptsPacket []common.Hash

// GetReceiptsPacket represents a block receipts query over eth/66.
type GetReceiptsPacket66 struct {
	RequestId uint64
	GetReceiptsPacket
}

// ReceiptsPacket is the network packet for block receipts distribution.
type ReceiptsPacket [][]*types.Receipt

// ReceiptsPacket is the network packet for block receipts distribution over eth/66.
type ReceiptsPacket66 struct {
	RequestId uint64
	ReceiptsPacket
}

// ReceiptsRLPPacket is used for receipts, when we already have it encoded
type ReceiptsRLPPacket []rlp.RawValue

// ReceiptsPacket66 is the eth-66 version of ReceiptsRLPPacket
type ReceiptsRLPPacket66 struct {
	RequestId uint64
	ReceiptsRLPPacket
}

// NewPooledTransactionHashesPacket represents a transaction announcement packet.
type NewPooledTransactionHashesPacket []common.Hash

// GetPooledTransactionsPacket represents a transaction query.
type GetPooledTransactionsPacket []common.Hash

type GetPooledTransactionsPacket66 struct {
	RequestId uint64
	GetPooledTransactionsPacket
}

// PooledTransactionsPacket is the network packet for transaction distribution.
type PooledTransactionsPacket []*types.Transaction

// PooledTransactionsPacket is the network packet for transaction distribution over eth/66.
type PooledTransactionsPacket66 struct {
	RequestId uint64
	PooledTransactionsPacket
}

// PooledTransactionsPacket is the network packet for transaction distribution, used
// in the cases we already have them in rlp-encoded form
type PooledTransactionsRLPPacket []rlp.RawValue

// PooledTransactionsRLPPacket66 is the eth/66 form of PooledTransactionsRLPPacket
type PooledTransactionsRLPPacket66 struct {
	RequestId uint64
	PooledTransactionsRLPPacket
}

// GetBlockPacket is the network packet for block fetching
type GetBlockPacket struct {
	Hash common.Hash
}

// GetBlockPacket66 is the eth/66 version of the GetBlockPacket.
type GetBlockPacket66 struct {
	RequestId uint64
	GetBlockPacket
}

// GetOnePendingEtxsPacket represents a pending etx query
type GetOnePendingEtxsPacket struct {
	Hash common.Hash
}

type GetOnePendingEtxsPacket66 struct {
	RequestId uint64
	GetOnePendingEtxsPacket
}

// GetOnePendingEtxsRollupPacket represents a pending etx query
type GetOnePendingEtxsRollupPacket struct {
	Hash common.Hash
}

type GetOnePendingEtxsRollupPacket66 struct {
	RequestId uint64
	GetOnePendingEtxsRollupPacket
}

type PendingEtxsPacket struct {
	PendingEtxs types.PendingEtxs
}

type PendingEtxsPacket66 struct {
	RequestId uint64
	PendingEtxsPacket
}

type PendingEtxsRollupPacket struct {
	PendingEtxsRollup types.PendingEtxsRollup
}

type PendingEtxsRollupPacket66 struct {
	RequestId uint64
	PendingEtxsRollupPacket
}

func (*StatusPacket) Name() string { return "Status" }
func (*StatusPacket) Kind() byte   { return StatusMsg }

func (*NewBlockHashesPacket) Name() string { return "NewBlockHashes" }
func (*NewBlockHashesPacket) Kind() byte   { return NewBlockHashesMsg }

func (*TransactionsPacket) Name() string { return "Transactions" }
func (*TransactionsPacket) Kind() byte   { return TransactionsMsg }

func (*GetBlockHeadersPacket) Name() string { return "GetBlockHeaders" }
func (*GetBlockHeadersPacket) Kind() byte   { return GetBlockHeadersMsg }

func (*BlockHeadersPacket) Name() string { return "BlockHeaders" }
func (*BlockHeadersPacket) Kind() byte   { return BlockHeadersMsg }

func (*GetBlockBodiesPacket) Name() string { return "GetBlockBodies" }
func (*GetBlockBodiesPacket) Kind() byte   { return GetBlockBodiesMsg }

func (*BlockBodiesPacket) Name() string { return "BlockBodies" }
func (*BlockBodiesPacket) Kind() byte   { return BlockBodiesMsg }

func (*NewBlockPacket) Name() string { return "NewBlock" }
func (*NewBlockPacket) Kind() byte   { return NewBlockMsg }

func (*GetNodeDataPacket) Name() string { return "GetNodeData" }
func (*GetNodeDataPacket) Kind() byte   { return GetNodeDataMsg }

func (*NodeDataPacket) Name() string { return "NodeData" }
func (*NodeDataPacket) Kind() byte   { return NodeDataMsg }

func (*GetReceiptsPacket) Name() string { return "GetReceipts" }
func (*GetReceiptsPacket) Kind() byte   { return GetReceiptsMsg }

func (*ReceiptsPacket) Name() string { return "Receipts" }
func (*ReceiptsPacket) Kind() byte   { return ReceiptsMsg }

func (*NewPooledTransactionHashesPacket) Name() string { return "NewPooledTransactionHashes" }
func (*NewPooledTransactionHashesPacket) Kind() byte   { return NewPooledTransactionHashesMsg }

func (*GetPooledTransactionsPacket) Name() string { return "GetPooledTransactions" }
func (*GetPooledTransactionsPacket) Kind() byte   { return GetPooledTransactionsMsg }

func (*PooledTransactionsPacket) Name() string { return "PooledTransactions" }
func (*PooledTransactionsPacket) Kind() byte   { return PooledTransactionsMsg }

func (*GetBlockPacket) Name() string { return "GetBlock" }
func (*GetBlockPacket) Kind() byte   { return GetBlockMsg }

func (*GetOnePendingEtxsPacket) Name() string { return "GetOnePendingEtxs" }
func (*GetOnePendingEtxsPacket) Kind() byte   { return GetOnePendingEtxsMsg }

func (*PendingEtxsPacket) Name() string { return "PendingEtxs" }
func (*PendingEtxsPacket) Kind() byte   { return PendingEtxsMsg }

func (*PendingEtxsRollupPacket) Name() string { return "PendingEtxsManifest" }
func (*PendingEtxsRollupPacket) Kind() byte   { return PendingEtxsRollupMsg }
