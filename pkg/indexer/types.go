package indexer

import (
	"strings"
	"time"

	"github.com/pkg/errors"
	"gorm.io/gorm"

	"github.com/gohornet/hornet/pkg/model/milestone"
	"github.com/iotaledger/hive.go/serializer/v2"
	iotago "github.com/iotaledger/iota.go/v3"
)

const (
	CursorLength = 76
)

var (
	NullOutputID = iotago.OutputID{}
)

type outputIDBytes []byte
type addressBytes []byte
type nftIDBytes []byte
type aliasIDBytes []byte
type foundryIDBytes []byte

type status struct {
	ID          uint `gorm:"primaryKey;notnull"`
	LedgerIndex milestone.Index
}

type queryResult struct {
	OutputID    outputIDBytes
	Cursor      string
	LedgerIndex milestone.Index
}

func (o outputIDBytes) ID() iotago.OutputID {
	id := iotago.OutputID{}
	copy(id[:], o)
	return id
}

type queryResults []queryResult

func (q queryResults) IDs() iotago.OutputIDs {
	outputIDs := iotago.OutputIDs{}
	for _, r := range q {
		outputIDs = append(outputIDs, r.OutputID.ID())
	}
	return outputIDs
}

func addressBytesForAddress(addr iotago.Address) (addressBytes, error) {
	return addr.Serialize(serializer.DeSeriModeNoValidation, nil)
}

type IndexerResult struct {
	OutputIDs   iotago.OutputIDs
	LedgerIndex milestone.Index
	PageSize    int
	Cursor      *string
	Error       error
}

func errorResult(err error) *IndexerResult {
	return &IndexerResult{
		Error: err,
	}
}

func unixTime(fromValue uint32) time.Time {
	return time.Unix(int64(fromValue), 0)
}

func (i *Indexer) combineOutputIDFilteredQuery(query *gorm.DB, pageSize int, cursor *string) *IndexerResult {

	query = query.Select("output_id").Order("created_at asc, output_id asc")
	if pageSize > 0 {
		query = query.Select("output_id", "printf('%08X', strftime('%s', `created_at`)) || hex(output_id) as cursor").Limit(pageSize + 1)

		if cursor != nil {
			if len(*cursor) != CursorLength {
				return errorResult(errors.Errorf("Invalid cursor length: %d", len(*cursor)))
			}
			query = query.Where("cursor >= ?", strings.ToUpper(*cursor))
		}
	}

	// This combines the query with a second query that checks for the current ledger_index.
	// This way we do not need to lock anything and we know the index matches the results.
	//TODO: measure performance for big datasets
	ledgerIndexQuery := i.db.Model(&status{}).Select("ledger_index")
	joinedQuery := i.db.Table("(?), (?)", query, ledgerIndexQuery)

	var results queryResults

	result := joinedQuery.Find(&results)
	if err := result.Error; err != nil {
		return errorResult(err)
	}

	ledgerIndex := milestone.Index(0)
	if len(results) > 0 {
		ledgerIndex = results[0].LedgerIndex
	}

	var nextCursor *string
	if pageSize > 0 && len(results) > pageSize {
		lastResult := results[len(results)-1]
		results = results[:len(results)-1]
		c := strings.ToLower(lastResult.Cursor)
		nextCursor = &c
	}

	return &IndexerResult{
		OutputIDs:   results.IDs(),
		LedgerIndex: ledgerIndex,
		PageSize:    pageSize,
		Cursor:      nextCursor,
		Error:       nil,
	}
}
