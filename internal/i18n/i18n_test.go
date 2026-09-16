package i18n

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestDetectFromChinese(t *testing.T) {
	cases := []string{
		"zh",
		"zh_CN",
		"zh_CN.UTF-8",
		"zh-TW",
		"zh_Hans",
		"ZH_CN.utf8",
	}
	for _, c := range cases {
		if got := DetectFrom(c); got != ZH {
			t.Errorf("DetectFrom(%q) = %q, want zh", c, got)
		}
	}
}

func TestDetectFromEnglishAndOthers(t *testing.T) {
	// 只提供中英双语，其他语言一律回退英文。
	cases := []string{"en", "en_US.UTF-8", "en_GB", "C", "POSIX", "C.UTF-8", "fr_FR.UTF-8", "ja_JP.UTF-8", "de"}
	for _, c := range cases {
		if got := DetectFrom(c); got != EN {
			t.Errorf("DetectFrom(%q) = %q, want en", c, got)
		}
	}
}

func TestDetectFromEmpty(t *testing.T) {
	if got := DetectFrom(); got != EN {
		t.Errorf("无任何取值时应默认英文, got %q", got)
	}
	if got := DetectFrom("", "", ""); got != EN {
		t.Errorf("空值应默认英文, got %q", got)
	}
}

func TestDetectFromPriority(t *testing.T) {
	// LC_ALL 优先于 LC_MESSAGES，LC_MESSAGES 优先于 LANG。
	if got := DetectFrom("zh_CN.UTF-8", "en_US.UTF-8", "en_US.UTF-8"); got != ZH {
		t.Errorf("LC_ALL 应优先, got %q", got)
	}
	if got := DetectFrom("", "zh_CN.UTF-8", "en_US.UTF-8"); got != ZH {
		t.Errorf("LC_MESSAGES 应在 LANG 之前生效, got %q", got)
	}
	if got := DetectFrom("", "", "zh_CN.UTF-8"); got != ZH {
		t.Errorf("LANG 应生效, got %q", got)
	}
	// LC_ALL=C 显式表示不做本地化，应压过 LANG=zh。
	if got := DetectFrom("C", "", "zh_CN.UTF-8"); got != EN {
		t.Errorf("LC_ALL=C 应压过 LANG=zh, got %q", got)
	}
}

func TestDetectReadsEnv(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "zh_CN.UTF-8")
	if got := Detect(); got != ZH {
		t.Errorf("Detect() = %q, want zh", got)
	}
	t.Setenv("LANG", "en_US.UTF-8")
	if got := Detect(); got != EN {
		t.Errorf("Detect() = %q, want en", got)
	}
}

func TestParseRecognisesConcreteLanguages(t *testing.T) {
	for _, c := range []string{"zh", "zh_CN", "zh-CN", "中文", "ZH"} {
		if l, ok := Parse(c); !ok || l != ZH {
			t.Errorf("Parse(%q) = (%q, %v), want (zh, true)", c, l, ok)
		}
	}
	for _, c := range []string{"en", "en_US", "English"} {
		if l, ok := Parse(c); !ok || l != EN {
			t.Errorf("Parse(%q) = (%q, %v), want (en, true)", c, l, ok)
		}
	}
	// auto / 空 / 未知取值都不算“具体语言”。
	for _, c := range []string{"", "auto", "AUTO", "fr", "klingon"} {
		if _, ok := Parse(c); ok {
			t.Errorf("Parse(%q) 不应被识别为具体语言", c)
		}
	}
}

func TestResolvePrefersExplicitConfigOverEnv(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "en_US.UTF-8")

	if got := Resolve("zh"); got != ZH {
		t.Errorf("配置 zh 应压过 LANG=en, got %q", got)
	}
	// auto 与空值都跟随环境变量。
	if got := Resolve("auto"); got != EN {
		t.Errorf("auto 应跟随 LANG=en, got %q", got)
	}
	if got := Resolve(""); got != EN {
		t.Errorf("空配置应跟随 LANG=en, got %q", got)
	}

	t.Setenv("LANG", "zh_CN.UTF-8")
	if got := Resolve("auto"); got != ZH {
		t.Errorf("auto 应跟随 LANG=zh, got %q", got)
	}
	if got := Resolve("en"); got != EN {
		t.Errorf("配置 en 应压过 LANG=zh, got %q", got)
	}
	// 无法识别的取值回退到环境变量而不是硬编码英文。
	if got := Resolve("fr"); got != ZH {
		t.Errorf("未知取值应回退到环境变量, got %q", got)
	}
}

func TestSetLangFallsBackToEnglish(t *testing.T) {
	defer SetLang(EN)
	SetLang(ZH)
	if Current() != ZH {
		t.Fatalf("Current() = %q, want zh", Current())
	}
	SetLang(Lang("klingon"))
	if Current() != EN {
		t.Errorf("未知语言应回退英文, got %q", Current())
	}
}

func TestTargetReturnsBothLanguages(t *testing.T) {
	zh := Target(ZH, "usage.line.requests", 3)
	en := Target(EN, "usage.line.requests", 3)
	if !strings.Contains(zh, "请求次数") || !strings.Contains(zh, "3") {
		t.Errorf("中文文案不正确: %q", zh)
	}
	if !strings.Contains(en, "Requests") || !strings.Contains(en, "3") {
		t.Errorf("英文文案不正确: %q", en)
	}
	if zh == en {
		t.Error("中英文文案不应相同")
	}
}

func TestTargetUnknownKeyReturnsKey(t *testing.T) {
	if got := Target(EN, "no.such.key"); got != "no.such.key" {
		t.Errorf("未知 key 应原样返回, got %q", got)
	}
}

func TestCatalogIsComplete(t *testing.T) {
	for _, k := range Keys() {
		e := catalog[k]
		if strings.TrimSpace(e.en) == "" {
			t.Errorf("key %q 缺少英文文案", k)
		}
		if strings.TrimSpace(e.zh) == "" {
			t.Errorf("key %q 缺少中文文案", k)
		}
	}
}

func TestQuoteKeepsUnicode(t *testing.T) {
	if got := Quote("你好"); got != "\"你好\"" {
		t.Errorf("Quote() = %q", got)
	}
}

// TestEveryUsedKeyIsDefined 扫描源码，确认所有 i18n.T("...") 用到的 key 都已定义。
//
// 这是防止漏翻译、拼错 key 的主要保障。
func TestEveryUsedKeyIsDefined(t *testing.T) {
	root := moduleRoot(t)
	used := map[string]string{} // key -> 首次出现的文件

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "dist" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "T" {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "i18n" {
				return true
			}
			if len(call.Args) == 0 {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			key, uerr := strconv.Unquote(lit.Value)
			if uerr != nil {
				return true
			}
			if _, seen := used[key]; !seen {
				used[key] = path
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("扫描源码失败: %v", err)
	}

	if len(used) == 0 {
		t.Fatal("未扫描到任何 i18n.T 调用，测试本身可能失效")
	}
	for key, file := range used {
		if _, ok := catalog[key]; !ok {
			t.Errorf("key %q 在 %s 中使用但未在 catalog 中定义", key, filepath.Base(file))
		}
	}
}

// moduleRoot 返回仓库根目录。
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("未能定位仓库根目录")
	return ""
}

// TestNoHardcodedChineseOutsideCatalog 扫描源码，禁止在 i18n 包之外出现
// 中文字符串字面量。
//
// 回归防护：曾经有若干提示(例如需求为空的报错)因为调用形式不同而漏改，
// 在 LANG=C 下仍然输出中文。文案必须集中在 catalog 中。
func TestNoHardcodedChineseOutsideCatalog(t *testing.T) {
	root := moduleRoot(t)
	han := regexp.MustCompile(`[\p{Han}\x{3000}-\x{303f}\x{ff00}-\x{ffef}]`)

	var offenders []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		// i18n 包本身是中文文案的唯一合法归属地。
		if filepath.Base(filepath.Dir(path)) == "i18n" {
			return nil
		}
		lits, lerr := chineseStringLiterals(path, han)
		if lerr != nil {
			return lerr
		}
		for _, l := range lits {
			offenders = append(offenders, filepath.ToSlash(path)+": "+l)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("扫描源码失败: %v", err)
	}
	for _, o := range offenders {
		t.Errorf("发现硬编码中文文案(应改用 i18n.T)：%s", o)
	}
}

// chineseStringLiterals 返回文件中所有含中文的字符串字面量(忽略注释)。
func chineseStringLiterals(path string, han *regexp.Regexp) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		val, uerr := strconv.Unquote(lit.Value)
		if uerr != nil {
			val = lit.Value
		}
		if han.MatchString(val) {
			out = append(out, lit.Value)
		}
		return true
	})
	return out, nil
}

// TestNoFullWidthParentheses 强制全项目使用半角括号。
//
// 这是维护者的个人偏好：即使是中文文案也统一用 `()`，不用全角写法。
// 测试用转义构造全角括号，避免本文件自身触发该规则。
func TestNoFullWidthParentheses(t *testing.T) {
	open := "\uff08"  // 全角左括号
	close := "\uff09" // 全角右括号

	root := moduleRoot(t)
	var offenders []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		switch {
		case strings.HasSuffix(path, ".go"),
			strings.HasSuffix(path, ".md"),
			info.Name() == "Makefile":
		default:
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		for i, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, open) || strings.Contains(line, close) {
				offenders = append(offenders, fmt.Sprintf("%s:%d: %s",
					filepath.ToSlash(path), i+1, strings.TrimSpace(line)))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("扫描文件失败: %v", err)
	}
	for _, o := range offenders {
		t.Errorf("发现全角括号，请改用半角 (): %s", o)
	}
}
