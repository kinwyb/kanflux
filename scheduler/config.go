package scheduler

import (
	"github.com/kinwyb/kanflux/config"
)

// SchedulerConfigAlias 别名，引用 config 包的 SchedulerConfig
// 这样 scheduler 包可以直接使用 config.SchedulerConfig
type SchedulerConfig = config.SchedulerConfig

// TaskConfigAlias 别名，引用 config 包的 TaskConfig
type TaskConfig = config.TaskConfig

// ScheduleConfigAlias 别名，引用 config 包的 ScheduleConfig
type ScheduleConfig = config.ScheduleConfig

// TargetConfigAlias 别名，引用 config 包的 TargetConfig
type TargetConfig = config.TargetConfig

// ContentConfigAlias 别名，引用 config 包的 ContentConfig
type ContentConfig = config.ContentConfig