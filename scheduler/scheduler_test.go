package scheduler

import (
	"testing"
	"time"
)

func TestParseCron(t *testing.T) {
	tests := []struct {
		name     string
		cron     string
		now      time.Time
		wantErr  bool
	}{
		{
			name:    "every day at 9am",
			cron:    "0 9 * * *",
			now:     time.Date(2026, 4, 17, 8, 30, 0, 0, time.Local),
			wantErr: false,
		},
		{
			name:    "every 30 minutes",
			cron:    "*/30 * * * *",
			now:     time.Date(2026, 4, 17, 9, 0, 0, 0, time.Local),
			wantErr: false,
		},
		{
			name:    "weekdays at 9am",
			cron:    "0 9 * * 1-5",
			now:     time.Date(2026, 4, 17, 8, 30, 0, 0, time.Local), // Friday
			wantErr: false,
		},
		{
			name:    "invalid cron - too few fields",
			cron:    "0 9 *",
			now:     time.Now(),
			wantErr: true,
		},
		{
			name:    "invalid cron - too many fields",
			cron:    "0 9 * * * *",
			now:     time.Now(),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextRun, err := parseCron(tt.cron, tt.now)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseCron(%s) expected error, got nil", tt.cron)
				}
				return
			}
			if err != nil {
				t.Errorf("parseCron(%s) unexpected error: %v", tt.cron, err)
				return
			}
			if nextRun.IsZero() {
				t.Errorf("parseCron(%s) returned zero time", tt.cron)
			}
			t.Logf("parseCron(%s) at %v -> nextRun: %v", tt.cron, tt.now, nextRun)
		})
	}
}

func TestParseCronField(t *testing.T) {
	tests := []struct {
		name    string
		field   string
		min     int
		max     int
		wantErr bool
		wantLen int // expected number of values (0 means don't check)
	}{
		{
			name:    "star - all values",
			field:   "*",
			min:     0,
			max:     59,
			wantErr: false,
			wantLen: 60,
		},
		{
			name:    "step - every 5 minutes",
			field:   "*/5",
			min:     0,
			max:     59,
			wantErr: false,
			wantLen: 12,
		},
		{
			name:    "range - 1-5",
			field:   "1-5",
			min:     0,
			max:     59,
			wantErr: false,
			wantLen: 5,
		},
		{
			name:    "list - 1,2,3",
			field:   "1,2,3",
			min:     0,
			max:     59,
			wantErr: false,
			wantLen: 3,
		},
		{
			name:    "specific value",
			field:   "30",
			min:     0,
			max:     59,
			wantErr: false,
			wantLen: 1,
		},
		{
			name:    "invalid value - out of range",
			field:   "60",
			min:     0,
			max:     59,
			wantErr: true,
		},
		{
			name:    "invalid range",
			field:   "5-1",
			min:     0,
			max:     59,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseCronField(tt.field, tt.min, tt.max)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseCronField(%s) expected error, got nil", tt.field)
				}
				return
			}
			if err != nil {
				t.Errorf("parseCronField(%s) unexpected error: %v", tt.field, err)
				return
			}
			if tt.wantLen > 0 && len(result) != tt.wantLen {
				t.Errorf("parseCronField(%s) expected %d values, got %d", tt.field, tt.wantLen, len(result))
			}
		})
	}
}

func TestParseInterval(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantErr  bool
		expected time.Duration
	}{
		{
			name:     "1 hour",
			input:    "1h",
			wantErr:  false,
			expected: time.Hour,
		},
		{
			name:     "30 minutes",
			input:    "30m",
			wantErr:  false,
			expected: 30 * time.Minute,
		},
		{
			name:     "1 day",
			input:    "1d",
			wantErr:  false,
			expected: 24 * time.Hour,
		},
		{
			name:     "5 seconds",
			input:    "5s",
			wantErr:  false,
			expected: 5 * time.Second,
		},
		{
			name:    "invalid - no unit",
			input:   "30",
			wantErr: true,
		},
		{
			name:    "invalid - unknown unit",
			input:   "30x",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseInterval(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseInterval(%s) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("parseInterval(%s) unexpected error: %v", tt.input, err)
				return
			}
			if result != tt.expected {
				t.Errorf("parseInterval(%s) expected %v, got %v", tt.input, tt.expected, result)
			}
		})
	}
}

func TestCalculateNextTime(t *testing.T) {
	// Test: every day at 9am
	now := time.Date(2026, 4, 17, 8, 30, 0, 0, time.Local)
	minute := []int{0}
	hour := []int{9}
	day := []int{} // all days (would be 1-31)
	month := []int{} // all months
	weekday := []int{} // all weekdays

	// Fill in the "all" values
	for i := 1; i <= 31; i++ {
		day = append(day, i)
	}
	for i := 1; i <= 12; i++ {
		month = append(month, i)
	}
	for i := 0; i <= 6; i++ {
		weekday = append(weekday, i)
	}

	nextRun, err := calculateNextTime(now, minute, hour, day, month, weekday)
	if err != nil {
		t.Fatalf("calculateNextTime error: %v", err)
	}

	// Expected: same day at 9:00
	expected := time.Date(2026, 4, 17, 9, 0, 0, 0, time.Local)
	if nextRun != expected {
		t.Errorf("expected %v, got %v", expected, nextRun)
	}

	// Test: if now is after 9am, should be next day
	now = time.Date(2026, 4, 17, 10, 0, 0, 0, time.Local)
	nextRun, err = calculateNextTime(now, minute, hour, day, month, weekday)
	if err != nil {
		t.Fatalf("calculateNextTime error: %v", err)
	}

	expected = time.Date(2026, 4, 18, 9, 0, 0, 0, time.Local)
	if nextRun != expected {
		t.Errorf("expected %v, got %v", expected, nextRun)
	}
}