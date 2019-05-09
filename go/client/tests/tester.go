// Package tests is a collection of client interface test cases.
package tests

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/oasislabs/ekiden/go/client"
	"github.com/oasislabs/ekiden/go/common/crypto/hash"
	"github.com/oasislabs/ekiden/go/common/crypto/signature"
	"github.com/oasislabs/ekiden/go/worker/compute/committee"
)

const timeout = 1 * time.Second

// ClientImplementationTests runs the client interface implementation tests.
func ClientImplementationTests(
	t *testing.T,
	client *client.Client,
	runtimeID signature.PublicKey,
	rtNode *committee.Node,
) {
	t.Run("SubmitTx", func(t *testing.T) {
		ctx, cancelFunc := context.WithTimeout(context.Background(), timeout)
		defer cancelFunc()
		testSubmitTransaction(ctx, t, runtimeID, client, rtNode)
	})

	t.Run("Query", func(t *testing.T) {
		ctx, cancelFunc := context.WithTimeout(context.Background(), timeout)
		defer cancelFunc()
		testQuery(ctx, t, runtimeID, client, rtNode)
	})

	// These can't test anything useful, so just make sure the roundtrip works.
	t.Run("WaitSync", func(t *testing.T) {
		ctx, cancelFunc := context.WithTimeout(context.Background(), timeout)
		defer cancelFunc()
		err := client.WaitSync(ctx)
		require.NoError(t, err, "WaitSync")
	})
	t.Run("IsSynced", func(t *testing.T) {
		ctx, cancelFunc := context.WithTimeout(context.Background(), timeout)
		defer cancelFunc()
		synced, err := client.IsSynced(ctx)
		require.NoError(t, err, "IsSynced")
		require.EqualValues(t, synced, true)
	})
}

func testSubmitTransaction(
	ctx context.Context,
	t *testing.T,
	runtimeID signature.PublicKey,
	c *client.Client,
	rtNode *committee.Node,
) {
	// Submit a test transaction.
	testInput := []byte("hello world")
	testOutput, err := c.SubmitTx(ctx, testInput, runtimeID)

	// Check if everything is in order.
	require.NoError(t, err, "SubmitTx")
	require.EqualValues(t, testInput, testOutput)
}

func testQuery(
	ctx context.Context,
	t *testing.T,
	runtimeID signature.PublicKey,
	c *client.Client,
	rtNode *committee.Node,
) {
	err := c.WaitBlockIndexed(ctx, runtimeID, 2)
	require.NoError(t, err, "WaitBlockIndexed")

	// Based on SubmitTx and the mock worker.
	testInput := []byte("hello world")
	testOutput := testInput

	// Fetch blocks.
	blk, err := c.GetBlock(ctx, runtimeID, 1)
	require.NoError(t, err, "GetBlock")
	require.EqualValues(t, 1, blk.Header.Round)

	blk, err = c.GetBlock(ctx, runtimeID, 2)
	require.NoError(t, err, "GetBlock")
	require.EqualValues(t, 2, blk.Header.Round)

	blk, err = c.GetBlock(ctx, runtimeID, 0xffffffffffffffff)
	require.NoError(t, err, "GetBlock")
	require.EqualValues(t, 2, blk.Header.Round)

	// Out of bounds block round.
	_, err = c.GetBlock(ctx, runtimeID, 3)
	require.Error(t, err, "GetBlock")

	// Fetch transaction.
	tx, err := c.GetTxn(ctx, runtimeID, 2, 0)
	require.NoError(t, err, "GetTxn(0)")
	require.EqualValues(t, 2, tx.Block.Header.Round)
	require.EqualValues(t, testInput, tx.Input)
	require.EqualValues(t, testOutput, tx.Output)

	// Out of bounds transaction index.
	_, err = c.GetTxn(ctx, runtimeID, 2, 1)
	require.Error(t, err, "GetTxn(1)")

	// Get transaction by block hash and index.
	tx, err = c.GetTxnByBlockHash(ctx, runtimeID, blk.Header.EncodedHash(), 0)
	require.NoError(t, err, "GetTxnByBlockHash")
	require.EqualValues(t, 2, tx.Block.Header.Round)
	require.EqualValues(t, testInput, tx.Input)
	require.EqualValues(t, testOutput, tx.Output)

	// Invalid block hash.
	var invalidHash hash.Hash
	invalidHash.Empty()
	_, err = c.GetTxnByBlockHash(ctx, runtimeID, invalidHash, 0)
	require.Error(t, err, "GetTxnByBlockHash(invalid)")

	// Check that indexer has indexed block keys (check the mock worker for key/values).
	blk, err = c.QueryBlock(ctx, runtimeID, []byte("foo"), []byte("bar"))
	require.NoError(t, err, "QueryBlock")
	require.EqualValues(t, 2, blk.Header.Round)

	// Check that indexer has indexed txn keys (check the mock worker for key/values).
	tx, err = c.QueryTxn(ctx, runtimeID, []byte("txn_foo"), []byte("txn_bar"))
	require.NoError(t, err, "QueryTxn")
	require.EqualValues(t, 2, tx.Block.Header.Round)
	require.EqualValues(t, 0, tx.Index)
	require.EqualValues(t, testInput, tx.Input)
	require.EqualValues(t, testOutput, tx.Output)

	// Transactions (check the mock worker for content).
	txns, err := c.GetTransactions(ctx, runtimeID, blk.Header.InputHash)
	require.NoError(t, err, "GetTransactions(input)")
	require.Len(t, txns, 1)
	require.EqualValues(t, testInput, txns[0])

	txns, err = c.GetTransactions(ctx, runtimeID, blk.Header.OutputHash)
	require.NoError(t, err, "GetTransactions(output)")
	require.Len(t, txns, 1)
	require.EqualValues(t, testOutput, txns[0])

	// Test advanced transaction queries.
	query := client.Query{
		RoundMin: 0,
		RoundMax: 3,
		Conditions: []client.QueryCondition{
			client.QueryCondition{Key: []byte("txn_foo"), Values: [][]byte{[]byte("txn_bar")}},
		},
	}
	results, err := c.QueryTxns(ctx, runtimeID, query)
	require.NoError(t, err, "QueryTxns")
	require.Len(t, results, 1)
	require.EqualValues(t, 2, results[0].Block.Header.Round)
	require.EqualValues(t, 0, results[0].Index)
	require.EqualValues(t, testInput, results[0].Input)
	require.EqualValues(t, testOutput, results[0].Output)
}