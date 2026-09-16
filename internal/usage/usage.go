// Package usage 按时间维度统计 Token 消耗与请求次数。
package usage

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/MrXie1109/command-ai/internal/history"
	"github.com/MrXie1109/command-ai/internal/i18n"
)

// Period 是统计的时间范围。
type Period string

// 支持的统计周期。
const (
	Today     Period = "today"
	ThisWeek  Period = "this-week"
	ThisMonth Period = "this-month"
	ThisYear  Period = "this-year"
	All       Period = "all"
)

// Periods 列出所有合法周期，用于帮助信息与校验。
var Periods = []Period{Today, ThisWeek, ThisMonth, ThisYear, All}

// ParsePeriod 解析命令行传入的周期参数，默认为 today。
func ParsePeriod(s string) (Period, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return Today, nil
	}
	for _, p := range Periods {
		if Period(s) == p {
			return p, nil
		}
	}
	return "", fmt.Errorf(i18n.T("usage.unknown_period"), i18n.Quote(s))
}

// Range 返回周期对应的起止时间(本地时区，含端点当天)。
func Range(p Period, now time.Time) (from, to time.Time) {
	to = now
	switch p {
	case Today:
		y, m, d := now.Date()
		from = time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	case ThisWeek:
		// 以周一为一周的开始。
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		y, m, d := now.AddDate(0, 0, -(weekday - 1)).Date()
		from = time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	case ThisMonth:
		y, m, _ := now.Date()
		from = time.Date(y, m, 1, 0, 0, 0, 0, now.Location())
	case ThisYear:
		y, _, _ := now.Date()
		from = time.Date(y, time.January, 1, 0, 0, 0, 0, now.Location())
	case All:
		from = time.Date(1970, 1, 1, 0, 0, 0, 0, time.Local)
	default:
		from = time.Date(1970, 1, 1, 0, 0, 0, 0, time.Local)
	}
	return from, to
}

// Stats 是统计结果。
type Stats struct {
	Period       Period
	From         time.Time
	To           time.Time
	Requests     int // 请求次数
	InputTokens  int
	OutputTokens int
	Executed     int // 实际执行成功的次数
}

// Total 返回 Token 总量。
func (s Stats) Total() int { return s.InputTokens + s.OutputTokens }

// Compute 汇总给定记录。
func Compute(p Period, recs []history.Record) Stats {
	st := Stats{Period: p}
	for _, r := range recs {
		// 每条记录携带本步骤的 LLM 请求次数；旧记录缺失该字段时按 1 次计。
		if r.LLMCalls > 0 {
			st.Requests += r.LLMCalls
		} else {
			st.Requests++
		}
		st.InputTokens += r.InputTokens
		st.OutputTokens += r.OutputTokens
		if r.Choice == history.ChoiceYes && r.Error == "" {
			st.Executed++
		}
	}
	return st
}

// Collect 从历史存储读取指定周期的记录并汇总。
func Collect(store *history.Store, p Period, now time.Time) (Stats, error) {
	from, to := Range(p, now)
	recs, err := store.LoadRange(from, to)
	if err != nil {
		return Stats{}, err
	}
	st := Compute(p, recs)
	st.From, st.To = from, to
	return st, nil
}

// PeriodLabel 返回周期的中文标签。
func PeriodLabel(p Period) string {
	switch p {
	case Today:
		return i18n.T("usage.period.today")
	case ThisWeek:
		return i18n.T("usage.period.this_week")
	case ThisMonth:
		return i18n.T("usage.period.this_month")
	case ThisYear:
		return i18n.T("usage.period.this_year")
	case All:
		return i18n.T("usage.period.all")
	default:
		return string(p)
	}
}

// Write 以固定格式输出统计结果。
func (s Stats) Write(w io.Writer) {
	if s.Period != All {
		fmt.Fprintf(w, i18n.T("usage.title_range"), PeriodLabel(s.Period),
			s.From.Format("2006-01-02"), s.To.Format("2006-01-02"))
	} else {
		fmt.Fprintf(w, i18n.T("usage.title_plain"), PeriodLabel(s.Period))
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, i18n.T("usage.line.requests"), s.Requests)
	fmt.Fprintf(w, i18n.T("usage.line.input"), s.InputTokens)
	fmt.Fprintf(w, i18n.T("usage.line.output"), s.OutputTokens)
	fmt.Fprintf(w, i18n.T("usage.line.total"), s.Total())
	fmt.Fprintf(w, i18n.T("usage.line.executed"), s.Executed)
}
