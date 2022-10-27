package vendors

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_TransferMetrics(t *testing.T) {
	cases := []struct {
		int64Key   string
		int64Value int64
		expected   map[string]int64
		clear      bool
		name       string
	}{
		{
			MetricTransferredBytes,
			100,
			map[string]int64{MetricTransferredBytes: 100},
			false,
			"new int64 key, value add",
		},
		{
			MetricTransferredBytes,
			100,
			map[string]int64{MetricTransferredBytes: 200},
			false,
			"int64 key, value increment",
		},
		{
			"",
			0,
			map[string]int64{},
			true,
			"clear values",
		},
	}

	metrics := NewMetrics()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.clear {
				metrics.Clear()
			} else {
				metrics.AddInt64Value(tc.int64Key, tc.int64Value)
			}
			assert.Equal(t, tc.expected, metrics.int64Values)
		})
	}
}
