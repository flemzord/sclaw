package cron

import (
	"testing"

	"github.com/robfig/cron/v3"
)

func FuzzCronSchedule(f *testing.F) {
	f.Add("*/5 * * * *")
	f.Add("0 0 * * *")
	f.Add("0 0 1 1 *")
	f.Add("* * * * *")
	f.Add("invalid")
	f.Add("")
	f.Add("60 * * * *")
	f.Add("0 25 * * *")

	f.Fuzz(func(_ *testing.T, expr string) {
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		// Must not panic — errors are expected and acceptable.
		_, _ = parser.Parse(expr)
	})
}
