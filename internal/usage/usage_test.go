package usage

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/command-ai/command-ai/internal/history"
	"github.com/command-ai/command-ai/internal/i18n"
)

func TestParsePeriod(t *testing.T) {
	cases := map[string]Period{
		"":           Today,
		"today":      Today,
		"TODAY":      Today,
		" this-week": ThisWeek,
		"this-month": ThisMonth,
		"this-year":  ThisYear,
		"all":        All,
	}
	for in, want := range cases {
		got, err := ParsePeriod(in)
		if err != nil {
			t.Errorf("ParsePeriod(%q) 报错: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParsePeriod(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := ParsePeriod("yesterday"); err == nil {
		t.Error("非法周期应报错")
	}
}

func TestRange(t *testing.T) {
	// 2024-05-15 是星期三，本周应为 5/13(周一) ~ 5/15。
	now := time.Date(2024, 5, 15, 14, 30, 0, 0, time.Local)

	from, _ := Range(Today, now)
	if from.Format("2006-01-02 15:04:05") != "2024-05-15 00:00:00" {
		t.Errorf("Today from = %v", from)
	}

	from, _ = Range(ThisWeek, now)
	if from.Format("2006-01-02") != "2024-05-13" {
		t.Errorf("ThisWeek from = %v, want 2024-05-13", from)
	}

	from, _ = Range(ThisMonth, now)
	if from.Format("2006-01-02") != "2024-05-01" {
		t.Errorf("ThisMonth from = %v", from)
	}

	from, _ = Range(ThisYear, now)
	if from.Format("2006-01-02") != "2024-01-01" {
		t.Errorf("ThisYear from = %v", from)
	}
}

func TestRangeWeekStartsOnMondayForSunday(t *testing.T) {
	// 2024-05-19 是星期日，本周应从 5/13(周一) 开始。
	sunday := time.Date(2024, 5, 19, 9, 0, 0, 0, time.Local)
	from, _ := Range(ThisWeek, sunday)
	if from.Format("2006-01-02") != "2024-05-13" {
		t.Errorf("周日的本周起点 = %v, want 2024-05-13", from)
	}
}

func TestCompute(t *testing.T) {
	code := 0
	recs := []history.Record{
		{Choice: history.ChoiceYes, InputTokens: 10, OutputTokens: 5, ExitCode: &code},
		{Choice: history.ChoiceNo, InputTokens: 20, OutputTokens: 8},
		{Choice: history.ChoiceRegen, InputTokens: 30, OutputTokens: 12},
		{Choice: history.ChoiceYes, InputTokens: 1, OutputTokens: 1, Error: "network"},
	}
	st := Compute(Today, recs)
	if st.Requests != 4 {
		t.Errorf("Requests = %d, want 4", st.Requests)
	}
	if st.InputTokens != 61 {
		t.Errorf("InputTokens = %d, want 61", st.InputTokens)
	}
	if st.OutputTokens != 26 {
		t.Errorf("OutputTokens = %d, want 26", st.OutputTokens)
	}
	if st.Total() != 87 {
		t.Errorf("Total = %d, want 87", st.Total())
	}
	// 只有第一条满足“选择 y 且无错误”。
	if st.Executed != 1 {
		t.Errorf("Executed = %d, want 1", st.Executed)
	}
}

func TestCollectReadsHistoryFromDisk(t *testing.T) {
	dir := t.TempDir()
	store, err := history.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	// 今天的 2 条 + 去年的 1 条。
	for _, ts := range []time.Time{now, now, now.AddDate(-1, 0, 0)} {
		if err := store.Append(history.Record{
			Timestamp:    ts,
			Input:        "x",
			Command:      "ls",
			Choice:       history.ChoiceYes,
			InputTokens:  7,
			OutputTokens: 3,
		}); err != nil {
			t.Fatal(err)
		}
	}

	today, err := Collect(store, Today, now)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if today.Requests != 2 {
		t.Errorf("今日 Requests = %d, want 2", today.Requests)
	}
	if today.InputTokens != 14 || today.OutputTokens != 6 {
		t.Errorf("今日 Token = %d/%d, want 14/6", today.InputTokens, today.OutputTokens)
	}

	all, err := Collect(store, All, now)
	if err != nil {
		t.Fatal(err)
	}
	if all.Requests != 3 {
		t.Errorf("全部 Requests = %d, want 3", all.Requests)
	}
}

func TestStatsWrite(t *testing.T) {
	i18n.SetLang(i18n.ZH)
	defer i18n.SetLang(i18n.EN)
	from := time.Date(2024, 5, 13, 0, 0, 0, 0, time.Local)
	to := time.Date(2024, 5, 15, 0, 0, 0, 0, time.Local)
	st := Stats{Period: ThisWeek, From: from, To: to, Requests: 3, InputTokens: 100, OutputTokens: 40, Executed: 2}

	var buf bytes.Buffer
	st.Write(&buf)
	out := buf.String()

	for _, want := range []string{"本周", "2024-05-13", "2024-05-15", "100", "40", "140", "3"} {
		if !strings.Contains(out, want) {
			t.Errorf("输出缺少 %q:\n%s", want, out)
		}
	}
}

func TestPeriodLabel(t *testing.T) {
	i18n.SetLang(i18n.ZH)
	defer i18n.SetLang(i18n.EN)
	if got := PeriodLabel(Today); got != "今日" {
		t.Errorf("PeriodLabel(Today) = %q", got)
	}
	if got := PeriodLabel(All); got != "全部" {
		t.Errorf("PeriodLabel(All) = %q", got)
	}
}

func TestComputeCountsLLMCallsPerRecord(t *testing.T) {
	// 一条记录可能合并了多次 LLM 调用（例如“解释 + 执行”）。
	recs := []history.Record{
		{Choice: history.ChoiceYes, LLMCalls: 2, InputTokens: 20, OutputTokens: 10},
		{Choice: history.ChoiceRegen, LLMCalls: 1, InputTokens: 10, OutputTokens: 5},
		// 旧记录没有 llm_calls 字段，应按 1 次计。
		{Choice: history.ChoiceNo, InputTokens: 10, OutputTokens: 5},
	}
	st := Compute(Today, recs)
	if st.Requests != 4 {
		t.Errorf("Requests = %d, want 4 (2+1+1)", st.Requests)
	}
	if st.InputTokens != 40 || st.OutputTokens != 20 {
		t.Errorf("Token = %d/%d, want 40/20", st.InputTokens, st.OutputTokens)
	}
}
