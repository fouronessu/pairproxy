package prompts

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewLoader(t *testing.T) {
	// 创建临时测试文件
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test_prompts.yaml")

	content := `
categories:
  test:
    prompts:
      - "prompt 1"
      - "prompt 2"
      - "prompt 3"
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	loader, err := NewLoader(testFile)
	if err != nil {
		t.Fatalf("NewLoader failed: %v", err)
	}

	// 验证加载成功
	if loader == nil {
		t.Fatal("Loader is nil")
	}

	// 验证 categories
	cats := loader.GetCategories()
	if len(cats) != 1 || cats[0] != "test" {
		t.Errorf("Expected categories [test], got %v", cats)
	}
}

func TestNewLoaderWithInvalidFile(t *testing.T) {
	loader, err := NewLoader("/nonexistent/file.yaml")
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
	if loader != nil {
		t.Error("Expected nil loader for error case")
	}
}

func TestLoaderGetRandom(t *testing.T) {
	data := map[string][]string{
		"code": {
			"Explain this code",
			"Refactor this function",
			"Debug this error",
		},
	}

	loader := NewLoaderWithData(data)

	// 多次获取随机 prompt，验证返回值在范围内
	for i := 0; i < 10; i++ {
		prompt := loader.GetRandom()
		found := false
		for _, p := range data["code"] {
			if p == prompt {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("GetRandom returned unexpected prompt: %s", prompt)
		}
	}
}

func TestLoaderGetRandomFromCategory(t *testing.T) {
	data := map[string][]string{
		"code": {
			"Explain this code",
			"Refactor this function",
		},
		"debug": {
			"Debug this error",
		},
	}

	loader := NewLoaderWithData(data)

	// 从存在的 category 获取
	prompt := loader.GetRandomFromCategory("code")
	if prompt == "" {
		t.Error("Expected non-empty prompt from existing category")
	}

	// 从不存在的 category 获取，应 fallback
	prompt = loader.GetRandomFromCategory("nonexistent")
	if prompt == "" {
		t.Error("Expected fallback to random prompt")
	}
}

func TestLoaderGetCategories(t *testing.T) {
	data := map[string][]string{
		"code":   {},
		"debug":  {},
		"design": {},
	}

	loader := NewLoaderWithData(data)
	cats := loader.GetCategories()

	if len(cats) != 3 {
		t.Errorf("Expected 3 categories, got %d", len(cats))
	}
}

func TestLoaderGetCategoryPrompts(t *testing.T) {
	data := map[string][]string{
		"code": {
			"prompt1",
			"prompt2",
		},
	}

	loader := NewLoaderWithData(data)

	// 获取存在的 category
	prompts := loader.GetCategoryPrompts("code")
	if len(prompts) != 2 {
		t.Errorf("Expected 2 prompts, got %d", len(prompts))
	}

	// 获取不存在的 category
	prompts = loader.GetCategoryPrompts("nonexistent")
	if prompts != nil {
		t.Error("Expected nil for nonexistent category")
	}
}

func TestDefaultPrompts(t *testing.T) {
	data := DefaultPrompts()

	// 验证有多个 category
	if len(data) == 0 {
		t.Error("DefaultPrompts returned empty data")
	}

	// 验证每个 category 有 prompts
	for cat, prompts := range data {
		if len(prompts) == 0 {
			t.Errorf("Category %s has no prompts", cat)
		}
	}
}
