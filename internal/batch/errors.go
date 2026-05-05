package batch

import "errors"

var (
	ErrQueueFull            = errors.New("batch: queue full")
	ErrAllFull              = errors.New("batch: all distributor queues full")
	ErrNoHealthyDistributor = errors.New("batch: no healthy distributor")
)
