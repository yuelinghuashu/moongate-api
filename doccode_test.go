package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// 本测试把文档里内嵌的 Go 代码块抽出来真正编译一遍，避免"文章里的代码早就编不过了"。
//
// 覆盖范围：content/docs/go-agent/（中英各一份）里带 package main 的代码块。
// 这些代码块是文章的核心交付物（读者复制即用），所以它们必须：
//  1. gofmt 干净；
//  2. go vet 通过（含编译）。
//
// 为什么只覆盖这一个系列：其他系列（lexer-from-scratch、gin-gorm、url-state 等）里也写了
// package main，但那是**教学片段**（伪代码、省略实现），本来就不是可编译单元——把它们纳入
// 会让本测试长期红灯。要推广到全站，得先把那些片段改成可编译的完整程序。
//
// 之所以放在测试里而不是单独脚本：CI（.github/workflows/deploy.yml）已经跑 go test ./...，
// 无需改动工作流即可获得持续校验。
const docCodeDir = "content/docs/go-agent"

// 只校验带 package main 的完整程序：片段（函数/结构体摘录）本来就不是可编译单元。
var goFenceRe = regexp.MustCompile("(?s)```go\n(.*?)```")

func TestDocGoCodeCompiles(t *testing.T) {
	blocks, err := collectDocGoCode(docCodeDir)
	if err != nil {
		t.Fatalf("扫描文档失败: %v", err)
	}
	if len(blocks) == 0 {
		t.Fatal("没有在文档里找到任何 package main 代码块，检查扫描规则是否失效")
	}

	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "go.mod"), []byte("module doccode\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("写 go.mod 失败: %v", err)
	}
	for i, b := range blocks {
		dir := filepath.Join(workDir, fmt.Sprintf("p%02d", i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("建目录失败: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(b.code), 0o644); err != nil {
			t.Fatalf("写 %s 失败: %v", b.where, err)
		}
	}

	// 1) gofmt：文档代码应当与 gofmt 输出一致（注意 go fmt 不支持 -l，要用 gofmt 本体）
	if out, err := runCmd(workDir, "gofmt", "-l", "."); err != nil {
		t.Fatalf("gofmt 执行失败: %v\n%s", err, out)
	} else if strings.TrimSpace(out) != "" {
		t.Errorf("以下文档代码块未通过 gofmt:\n%s", out)
	}

	// 2) go vet：包含编译，能同时抓出类型错误与常见问题
	if out, err := runGo(workDir, "vet", "./..."); err != nil {
		t.Errorf("文档代码未能通过 go vet（%d 个代码块）:\n%s", len(blocks), out)
	}
}

type docCode struct {
	where string // 出自哪个文件
	code  string
}

// collectDocGoCode 递归读取目录下的 markdown，抽出带 package main 的 go 代码块。
// 英文译文（*.en.md）与中文正文内容一致，一并校验，防止翻译时改动代码而未被发现。
func collectDocGoCode(root string) ([]docCode, error) {
	var out []docCode
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, m := range goFenceRe.FindAllStringSubmatch(string(raw), -1) {
			code := m[1]
			if !strings.Contains(code, "package main") {
				continue
			}
			out = append(out, docCode{
				where: fmt.Sprintf("%s（第 %d 个 go 代码块）", path, i+1),
				code:  code,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].where < out[j].where })
	return out, nil
}

func runGo(dir string, args ...string) (string, error) {
	return runCmd(dir, "go", args...)
}

func runCmd(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	// 用临时 GOCACHE，避免与开发机的构建缓存互相干扰
	cmd.Env = append(os.Environ(), "GOCACHE="+filepath.Join(dir, ".gocache"))
	out, err := cmd.CombinedOutput()
	return string(out), err
}
