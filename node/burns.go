package node

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/FactomProject/factom"
	"github.com/pegnet/pegnet-node/node/database"
	"github.com/pegnet/pegnet/balances"
	"github.com/pegnet/pegnet/common"
	log "github.com/sirupsen/logrus"
	"github.com/zpatrick/go-config"
)

// TODO: Until the original burns have an easy way to export the deltas,
//		a little copy paste of the burns will be done

type NodeBurnTracking struct {
	FctDbht  int64
	Balances *balances.BalanceTracker
	Node     *PegnetNode
}

func NewNodeBurnTracking(balanceTracker *balances.BalanceTracker, n *PegnetNode) *NodeBurnTracking {
	b := new(NodeBurnTracking)
	b.Balances = balanceTracker
	b.Node = n

	return b
}

func (b *NodeBurnTracking) UpdateBurns(c *config.Config, startBlock int64) error {
	network, err := common.LoadConfigNetwork(c)
	if err != nil {
		panic("cannot find the network designation for updating burn txs")
	}

	if b.FctDbht == 0 {
		b.FctDbht = startBlock
	}

	heights, err := factom.GetHeights()
	if err != nil {
		return err
	}

	flog := log.WithFields(log.Fields{"id": "burns", "top": heights.DirectoryBlockHeight, "start": b.FctDbht})
	flog.Info("Start burn syncing")
	for i := b.FctDbht + 1; i < heights.DirectoryBlockHeight; i++ {
		deltas := make(map[string]int64)
		totalBurned := int64(0)

		fc, _, err := factom.GetFBlockByHeight(i)
		if err != nil {
			return err
		}
		if fc == nil {
			return fmt.Errorf("fblock is nil")
		}

		for _, txid := range fc.Transactions {
			txInterface, err := factom.GetTransaction(txid.TxID)
			if err != nil {
				return err
			}

			txData, err := json.Marshal(txInterface.FactoidTransaction)
			if err != nil {
				return err
			}

			tx := new(FactoidTransaction)
			err = json.Unmarshal(txData, tx)
			if err != nil {
				return err
			}

			// Is this a burn?
			if len(tx.Outecs) == 1 && tx.Outecs[0].Useraddress == common.BurnAddresses[network] && tx.Outecs[0].Amount == 0 {
				// The output is a burn. Let's check some other properties
				if len(tx.Outputs) > 0 || len(tx.Inputs) > 1 {
					continue // must only have 1 output, and 1 input, being the burn
				}

				burnAmt := tx.Inputs[0].Amount
				pFct, err := common.ConvertFCTtoPegNetAsset(network, "FCT", tx.Inputs[0].Useraddress)
				if err != nil {
					return err
				}
				if network == common.MainNetwork {
					deltas[pFct] += int64(burnAmt)
					totalBurned += int64(burnAmt)

				} else if network == common.TestNetwork {
					deltas[pFct] += int64(burnAmt) * 1000
					totalBurned += int64(burnAmt) * 1000
				}

			}
		}

		// Add to node sql db
		t, err := database.TimeSeriesFromHeight(i)
		if err != nil {
			return fmt.Errorf("failed to get time series: %s", err.Error()) // Cancel height apply, it failed
		}

		addrList, deltaList := make([]string, len(deltas)), make([]string, len(deltas))
		count := 0
		for k, v := range deltas {
			addrList[count] = k
			deltaList[count] = fmt.Sprintf("%d", v)
			count++
		}

		burns := database.FCTBurnsTimeSeries{
			TimeSeries:  t,
			TotalBurned: totalBurned,
			Addresses:   strings.Join(addrList, ","),
			Amounts:     strings.Join(deltaList, ","),
		}

		tx := b.Node.NodeDatabase.DB.Begin()
		err = database.InsertTimeSeries(tx, &burns)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("%d %s", i, common.DetailError(err))
		}

		dberr := tx.Commit()
		if dberr.Error != nil {
			return dberr.Error
		}

		// Process them as a block
		for pFct, delta := range deltas {
			_ = b.Balances.AddToBalance(pFct, delta)
		}

		b.FctDbht = i
	}

	flog.Info("Done burn syncing")

	return nil
}

type FactoidTransaction struct {
	Millitimestamp int64               `json:"millitimestamp"`
	Inputs         []TransactionOutput `json:"inputs"`
	Outputs        []TransactionOutput `json:"outputs"`
	Outecs         []TransactionOutput `json:"outecs"`
	Rcds           []string            `json:"rcds"`
	Sigblocks      []struct {
		Signatures []string `json:"signatures"`
	} `json:"sigblocks"`
	Blockheight int `json:"blockheight"`
}

type TransactionOutput struct {
	Amount      int64  `json:"amount"`
	Address     string `json:"address"`
	Useraddress string `json:"useraddress"`
}
