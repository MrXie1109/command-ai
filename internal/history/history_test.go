package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func TestAppendCreatesDailyFile(t *testing.T) {
	s := newTestStore(t)
	ts := time.Date(2024, 5, 17, 10, 30, 0, 0, time.Local)

	code := 0
	rec := Record{
		Timestamp:    ts,
		Input:        "列出文件",
		Command:      "ls",
		Choice:       ChoiceYes,
		Output:       "a.txt\nb.txt\n",
		ExitCode:     &code,
		InputTokens:  100,
		OutputTokens: 20,
		Model:        "deepseek-flash",
	}
	if err := s.Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}

	path := filepath.Join(s.Dir, "2024-05-17.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取历史文件: %v", err)
	}

	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}
	if lines != 1 {
		t.Errorf("应有 1 行记录, got %d", lines)
	}

	var got Record
	if err := json.Unmarshal(data[:len(data)-1], &got); err != nil {
		t.Fatalf("JSON 解析: %v", err)
	}
	if got.Input != rec.Input || got.Command != rec.Command || got.Choice != ChoiceYes {
		t.Errorf("记录内容不匹配: %+v", got)
	}
	if got.InputTokens != 100 || got.OutputTokens != 20 {
		t.Errorf("Token 不匹配: %+v", got)
	}
}

func TestAppendIsAppendOnly(t *testing.T) {
	s := newTestStore(t)
	ts := time.Date(2024, 5, 17, 10, 30, 0, 0, time.Local)
	for i := 0; i < 3; i++ {
		if err := s.Append(Record{Timestamp: ts, Input: "x", Command: "ls"}); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	recs, err := s.LoadRange(ts.AddDate(0, 0, -1), ts.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("LoadRange: %v", err)
	}
	if len(recs) != 3 {
		t.Errorf("应有 3 条记录, got %d", len(recs))
	}
}

func TestFileAndDirPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 使用 ACL")
	}
	s := newTestStore(t)
	ts := time.Now()
	if err := s.Append(Record{Timestamp: ts, Input: "x", Command: "ls"}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	di, err := os.Stat(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("目录权限 = %o, want 700", perm)
	}

	fi, err := os.Stat(s.pathFor(ts))
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("文件权限 = %o, want 600", perm)
	}
}

func TestLoadRangeFiltersByDate(t *testing.T) {
	s := newTestStore(t)
	days := []time.Time{
		time.Date(2024, 1, 1, 12, 0, 0, 0, time.Local),
		time.Date(2024, 1, 5, 12, 0, 0, 0, time.Local),
		time.Date(2024, 1, 10, 12, 0, 0, 0, time.Local),
	}
	for _, d := range days {
		if err := s.Append(Record{Timestamp: d, Input: d.Format("01-02"), Command: "ls"}); err != nil {
			t.Fatal(err)
		}
	}

	// 含端点：1/1 ~ 1/5 应覆盖前两天。
	recs, err := s.LoadRange(days[0], days[1])
	if err != nil {
		t.Fatalf("LoadRange: %v", err)
	}
	if len(recs) != 2 {
		t.Errorf("应有 2 条记录, got %d", len(recs))
	}

	// 全天范围应包含全部 3 条。
	all, err := s.LoadRange(time.Date(2000, 1, 1, 0, 0, 0, 0, time.Local), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Errorf("应有 3 条记录, got %d", len(all))
	}
}

func TestLoadRangeMissingDirIsEmpty(t *testing.T) {
	s := &Store{Dir: filepath.Join(t.TempDir(), "nope")}
	recs, err := s.LoadRange(time.Now().AddDate(0, 0, -1), time.Now())
	if err != nil {
		t.Fatalf("目录不存在不应报错: %v", err)
	}
	if len(recs) != 0 {
		t.Errorf("应为空, got %d", len(recs))
	}
}

func TestLoadSkipsCorruptLines(t *testing.T) {
	s := newTestStore(t)
	ts := time.Now()
	if err := s.Append(Record{Timestamp: ts, Input: "good", Command: "ls"}); err != nil {
		t.Fatal(err)
	}
	// 追加一行损坏内容，不应影响后续读取。
	f, err := os.OpenFile(s.pathFor(ts), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("{not json}\n")
	f.Close()
	if err := s.Append(Record{Timestamp: ts, Input: "good2", Command: "pwd"}); err != nil {
		t.Fatal(err)
	}

	recs, err := s.LoadRange(ts, ts)
	if err != nil {
		t.Fatalf("LoadRange: %v", err)
	}
	if len(recs) != 2 {
		t.Errorf("损坏行应被跳过，期望 2 条, got %d", len(recs))
	}
}

func TestAppendSetsTimestampWhenZero(t *testing.T) {
	s := newTestStore(t)
	if err := s.Append(Record{Input: "x", Command: "ls"}); err != nil {
		t.Fatal(err)
	}
	recs, err := s.LoadRange(time.Now().AddDate(0, 0, -1), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("应有 1 条, got %d", len(recs))
	}
	if recs[0].Timestamp.IsZero() {
		t.Error("时间戳应被自动填充")
	}
}

func TestNewCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b", "history")
	if _, err := New(dir); err != nil {
		t.Fatalf("New: %v", err)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Errorf("目录未被创建: %v", err)
	}
}

func TestDefaultDirHonoursEnv(t *testing.T) {
	t.Setenv("COMMAND_AI_HOME", "/tmp/cai-home")
	want := filepath.Join("/tmp/cai-home", "history")
	if got := DefaultDir(); got != want {
		t.Errorf("DefaultDir() = %q, want %q", got, want)
	}
}
