package processor

import (
	"github.com/kinwyb/kanflux/memoria/types"
)

// BaseProcessor provides common functionality for processors
type BaseProcessor struct {
	Summarizer types.Summarizer
	Config     *types.ProcessorConfig
}

// ProcessorConfig alias for backward compatibility
type ProcessorConfig = types.ProcessorConfig

// NewBaseProcessor creates a base processor
func NewBaseProcessor(summarizer types.Summarizer, config *ProcessorConfig) *BaseProcessor {
	return &BaseProcessor{
		Summarizer: summarizer,
		Config:     config,
	}
}
