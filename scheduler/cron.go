package scheduler

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// parseCron 解析 cron 表达式，计算下次执行时间
// 支持标准5字段格式: 分 时 日 月 周
// 示例: "0 9 * * *" -> 每天9点
//       "*/30 * * * *" -> 每30分钟
//       "0 9 * * 1-5" -> 工作日9点
func parseCron(cron string, now time.Time) (time.Time, error) {
	fields := strings.Fields(cron)
	if len(fields) != 5 {
		return time.Time{}, fmt.Errorf("invalid cron format: expected 5 fields, got %d", len(fields))
	}

	// 解析各字段
	minute, err := parseCronField(fields[0], 0, 59)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid minute field: %w", err)
	}

	hour, err := parseCronField(fields[1], 0, 23)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid hour field: %w", err)
	}

	day, err := parseCronField(fields[2], 1, 31)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid day field: %w", err)
	}

	month, err := parseCronField(fields[3], 1, 12)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid month field: %w", err)
	}

	weekday, err := parseCronField(fields[4], 0, 6)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid weekday field: %w", err)
	}

	// 计算下次执行时间
	return calculateNextTime(now, minute, hour, day, month, weekday)
}

// parseCronField 解析单个 cron 字段
// 支持: *, 具体值, 范围(1-5), 步进(*/5)
func parseCronField(field string, min, max int) ([]int, error) {
	if field == "*" {
		// 所有值
		result := make([]int, max-min+1)
		for i := min; i <= max; i++ {
			result[i-min] = i
		}
		return result, nil
	}

	// 步进: */5
	if strings.HasPrefix(field, "*/") {
		stepStr := strings.TrimPrefix(field, "*/")
		step, err := strconv.Atoi(stepStr)
		if err != nil {
			return nil, fmt.Errorf("invalid step value: %s", stepStr)
		}
		if step <= 0 {
			return nil, fmt.Errorf("step must be positive: %d", step)
		}
		result := make([]int, 0)
		for i := min; i <= max; i += step {
			result = append(result, i)
		}
		return result, nil
	}

	// 列表: 1,2,3
	if strings.Contains(field, ",") {
		parts := strings.Split(field, ",")
		result := make([]int, 0, len(parts))
		for _, part := range parts {
			val, err := strconv.Atoi(strings.TrimSpace(part))
			if err != nil {
				return nil, fmt.Errorf("invalid value in list: %s", part)
			}
			if val < min || val > max {
				return nil, fmt.Errorf("value %d out of range [%d, %d]", val, min, max)
			}
			result = append(result, val)
		}
		return result, nil
	}

	// 范围: 1-5
	if strings.Contains(field, "-") {
		parts := strings.Split(field, "-")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid range format: %s", field)
		}
		start, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		end, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("invalid range values: %s", field)
		}
		if start < min || end > max || start > end {
			return nil, fmt.Errorf("range %d-%d invalid for bounds [%d, %d]", start, end, min, max)
		}
		result := make([]int, end-start+1)
		for i := start; i <= end; i++ {
			result[i-start] = i
		}
		return result, nil
	}

	// 具体值
	val, err := strconv.Atoi(field)
	if err != nil {
		return nil, fmt.Errorf("invalid value: %s", field)
	}
	if val < min || val > max {
		return nil, fmt.Errorf("value %d out of range [%d, %d]", val, min, max)
	}
	return []int{val}, nil
}

// calculateNextTime 计算下次执行时间
func calculateNextTime(now time.Time, minute, hour, day, month, weekday []int) (time.Time, error) {
	// 从当前时间的下一分钟开始查找
	next := now.Add(time.Minute).Truncate(time.Second)

	// 最多尝试 366 天（一年）
	maxAttempts := 366 * 24 * 60
	for i := 0; i < maxAttempts; i++ {
		if matchesCron(next, minute, hour, day, month, weekday) {
			return next, nil
		}
		next = next.Add(time.Minute)
	}

	return time.Time{}, fmt.Errorf("no matching time found within next year")
}

// matchesCron 检查时间是否匹配 cron 表达式
func matchesCron(t time.Time, minute, hour, day, month, weekday []int) bool {
	// 检查分钟
	if !containsInt(minute, t.Minute()) {
		return false
	}

	// 检查小时
	if !containsInt(hour, t.Hour()) {
		return false
	}

	// 检查日
	if !containsInt(day, t.Day()) {
		return false
	}

	// 检查月
	if !containsInt(month, int(t.Month())) {
		return false
	}

	// 检查星期 (cron 使用 0=Sunday, 6=Saturday)
	// Go 的 Weekday(): 0=Sunday, 6=Saturday，正好匹配
	if !containsInt(weekday, int(t.Weekday())) {
		return false
	}

	return true
}

// containsInt 检查整数是否在列表中
func containsInt(list []int, val int) bool {
	for _, v := range list {
		if v == val {
			return true
		}
	}
	return false
}

// parseInterval 解析间隔字符串
// 支持: "1h", "30m", "24h", "1d"
func parseInterval(interval string) (time.Duration, error) {
	re := regexp.MustCompile(`^(\d+)([hmsd])$`)
	matches := re.FindStringSubmatch(interval)
	if len(matches) != 3 {
		return 0, fmt.Errorf("invalid interval format: %s (expected format like '1h', '30m', '5s', '1d')", interval)
	}

	val, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, fmt.Errorf("invalid interval value: %s", matches[1])
	}

	unit := matches[2]

	switch unit {
	case "h":
		return time.Duration(val) * time.Hour, nil
	case "m":
		return time.Duration(val) * time.Minute, nil
	case "s":
		return time.Duration(val) * time.Second, nil
	case "d":
		return time.Duration(val) * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unknown interval unit: %s", unit)
	}
}

// validateCron 验证 cron 表达式是否有效
func validateCron(cron string) error {
	_, err := parseCron(cron, time.Now())
	return err
}