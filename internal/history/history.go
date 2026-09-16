// Package history 以 JSONL 形式按天记录每次请求。
//
// 文件路径：<数据目录>/history/YYYY-MM-DD.jsonl，文件权限 0600，目录 0700。
package history

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/command-ai/command-ai/internal/i18n"
)

// Choice 是用户在某次交互中的最终选择。
type Choice string

// 用户可能的选择。
const (
	ChoiceYes     Choice = "y" // 确认执行
	ChoiceNo      Choice = "n" // 取消
	ChoiceError   Choice = "err"
	ChoiceEmpty   Choice = "" // 未做出选择(例如解释后退出)
	ChoiceRegen   Choice = "r"
	ChoiceExplain Choice = "e"
)

// Record 是一条历史记录(一行 JSON)。
//
// InputTokens / OutputTokens / LLMCalls 记录的是「自上一条记录以来」这一步
// 消耗的用量(而非整个会话的累计值)，这样把所有记录相加即得到真实总量，
// 不会因为 e(解释)/ r(重新生成)而重复计数。
type Record struct {
	Timestamp    time.Time `json:"timestamp"`
	Input        string    `json:"input"`            // 用户输入(自然语言)
	Command      string    `json:"command"`          // 生成的命令
	Choice       Choice    `json:"choice"`           // 用户选择 y/n/e/r
	Output       string    `json:"output,omitempty"` // 命令输出(可选)
	ExitCode     *int      `json:"exit_code,omitempty"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	// LLMCalls 是本步骤消耗的 LLM 请求次数(一次“解释 + 执行”可能合并为一步)。
	// 旧记录可能缺少该字段，统计时按 1 次处理。
	LLMCalls int    `json:"llm_calls,omitempty"`
	Model    string `json:"model,omitempty"`
	// ModelError 记录模型拒答时的原因(即 "Error:" 分支的内容)，
	// 与执行失败 Error 区分开。
	ModelError string `json:"model_error,omitempty"`
	Error      string `json:"error,omitempty"` // 失败原因(网络错误等)
}

// Store 是历史记录的存储目录。
type Store struct {
	Dir string

	mu sync.Mutex
}

// DefaultDir 返回默认的历史目录。
func DefaultDir() string {
	if p := os.Getenv("COMMAND_AI_HOME"); p != "" {
		return filepath.Join(p, "history")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "history"
	}
	return filepath.Join(home, ".local", "share", "command-ai", "history")
}

// New 创建 Store 并确保目录存在(权限 0700)。
func New(dir string) (*Store, error) {
	if dir == "" {
		dir = DefaultDir()
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf(i18n.T("history.mkdir_failed"), err)
	}
	// MkdirAll 对已存在目录不改变权限，这里确保一次。
	if err := os.Chmod(dir, 0o700); err != nil && !os.IsPermission(err) {
		// Windows 上可能不支持，忽略即可。
		_ = err
	}
	return &Store{Dir: dir}, nil
}

// pathFor 返回某一天的历史文件路径。
func (s *Store) pathFor(t time.Time) string {
	return filepath.Join(s.Dir, t.Format("2006-01-02")+".jsonl")
}

// Append 追加一条记录到对应日期的文件中。
func (s *Store) Append(rec Record) error {
	if rec.Timestamp.IsZero() {
		rec.Timestamp = time.Now()
	}

	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf(i18n.T("history.marshal_failed"), err)
	}
	data = append(data, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.pathFor(rec.Timestamp)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf(i18n.T("history.open_failed"), path, err)
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf(i18n.T("history.write_failed"), err)
	}
	return nil
}

// LoadRange 读取 [from, to] 区间(含端点，按日期)内的所有记录。
func (s *Store) LoadRange(from, to time.Time) ([]Record, error) {
	var out []Record

	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf(i18n.T("history.readdir_failed"), err)
	}

	from = truncateDay(from)
	to = truncateDay(to)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".jsonl" {
			continue
		}
		day, err := time.ParseInLocation("2006-01-02", trimExt(e.Name()), time.Local)
		if err != nil {
			continue
		}
		if day.Before(from) || day.After(to) {
			continue
		}
		recs, err := s.loadFile(filepath.Join(s.Dir, e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, recs...)
	}
	return out, nil
}

// loadFile 读取单个 JSONL 文件。损坏的行会被跳过，以免影响统计。
func (s *Store) loadFile(path string) ([]Record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf(i18n.T("history.open_failed"), path, err)
	}
	defer f.Close()

	var recs []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec Record
		if err := json.Unmarshal(line, &rec); err != nil {
			continue // 跳过损坏行
		}
		recs = append(recs, rec)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf(i18n.T("history.scan_failed"), path, err)
	}
	return recs, nil
}

func truncateDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func trimExt(name string) string {
	return name[:len(name)-len(filepath.Ext(name))]
}
