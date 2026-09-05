package csv_test

import (
	"testing"
	"time"

	"github.com/theflywheel/crest/adapters"
	"github.com/theflywheel/crest/adapters/contract"
	"github.com/theflywheel/crest/adapters/csv"
	"github.com/theflywheel/crest/pkg/schema"
)

func TestConnectorContract(t *testing.T) {
	received := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	contract.Run(t, contract.Suite{
		Adapter: csv.Adapter{},
		Source: adapters.Source{
			Class: schema.SourceClassProgrammeSystem, CaptureMethod: schema.CaptureMethodDigitalCapture,
			Exposure: schema.SourceExposureSignedBatch, SystemRef: "contract-fixture",
		},
		ReceivedAt: received,
		Cases: []contract.Case{{
			Name: "valid and rejected rows are accounted for",
			Payload: []byte("activity,outcome_value,outcome_unit,worker_id_kind,worker_id,period_start\n" +
				"bednet-distribution,4,bednets-distributed,phone,+15550100011,2026-09-04\n" +
				"bednet-distribution,not-a-number,bednets-distributed,phone,+15550100012,2026-09-04\n"),
			WantRows: 1, WantRejections: 1,
		}},
	})
}
