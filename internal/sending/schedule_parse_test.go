package sending_test

import (
	"testing"
	"time"

	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

func TestParseScheduleDateAndClock(t *testing.T) {
	tests := []struct {
		name, date, clock string
		want              time.Time
		wantErr           bool
	}{
		{name: "midnight in India", date: "2026-10-22", clock: "00:00", want: time.Date(2026, time.October, 21, 18, 30, 0, 0, time.UTC)},
		{name: "noon in India", date: "2026-10-22", clock: "12:00", want: time.Date(2026, time.October, 22, 6, 30, 0, 0, time.UTC)},
		{name: "invalid date", date: "2026-02-30", clock: "09:15", wantErr: true},
		{name: "invalid clock hour", date: "2026-10-22", clock: "24:00", wantErr: true},
		{name: "invalid clock minute", date: "2026-10-22", clock: "09:60", wantErr: true},
		{name: "malformed clock", date: "2026-10-22", clock: "9:5", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sending.ParseSchedule(tt.date, tt.clock)
			if tt.wantErr {
				if err == nil {
					t.Fatal("ParseSchedule accepted invalid date or clock")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("ParseSchedule() = %s, want %s", got, tt.want)
			}
			if got.Location() != time.UTC {
				t.Errorf("ParseSchedule() location = %v, want UTC", got.Location())
			}
		})
	}
}
