package services

import (
	"path/filepath"
	"strings"
	"testing"

	"pvfine/internal/pvf"
)

func TestCompareService(t *testing.T) {
	tempDir := t.TempDir()
	basePath := filepath.Join(tempDir, "base.pvf")
	targetPath := filepath.Join(tempDir, "target.pvf")

	// 构建 base 归档
	base := pvf.New()
	base.AddFileText("script/common.txt", "[name]\n`same`\n", pvf.TypeScript)
	base.AddFileText("script/modified.txt", "[name]\n`old_version`\n[val]\n1\n", pvf.TypeScript)
	base.AddFileText("script/to_delete.txt", "[name]\n`deleted`\n", pvf.TypeScript)
	base.AddFile("bin/raw.bin", []byte{1, 2, 3, 4}, 0) // 真实非脚本二进制文件 (dataType 0)
	if err := base.SaveAs(basePath); err != nil {
		t.Fatalf("保存 base.pvf 失败: %v", err)
	}

	// 构建 target 归档
	target := pvf.New()
	target.AddFileText("script/common.txt", "[name]\n`same`\n", pvf.TypeScript)
	target.AddFileText("script/modified.txt", "[name]\n`new_version`\n[val]\n2\n", pvf.TypeScript)
	target.AddFileText("script/added.txt", "[name]\n`new_file`\n", pvf.TypeScript)
	target.AddFile("bin/raw.bin", []byte{9, 8, 7, 6}, 0) // 修改后的非脚本二进制文件
	if err := target.SaveAs(targetPath); err != nil {
		t.Fatalf("保存 target.pvf 失败: %v", err)
	}

	core := NewCore()
	service := NewCompareService(core)

	// 0. 未打开归档时应该报错
	if _, err := service.Compare("", targetPath, true); err == nil {
		t.Errorf("未打开归档时进行 Compare 期望返回错误")
	}

	// 打开 base 归档作为当前工作区
	openedBase, err := pvf.Open(basePath)
	if err != nil {
		t.Fatalf("打开 base 失败: %v", err)
	}
	if err := core.setArchive(openedBase); err != nil {
		t.Fatalf("setArchive 失败: %v", err)
	}

	// 1. 测试与目标外部归档对比
	summary, err := service.Compare("", targetPath, true)
	if err != nil {
		t.Fatalf("Compare 失败: %v", err)
	}

	if summary.AddCount != 1 {
		t.Errorf("AddCount = %d, 期望 1", summary.AddCount)
	}
	if summary.DeleteCount != 1 {
		t.Errorf("DeleteCount = %d, 期望 1", summary.DeleteCount)
	}
	if summary.ModifyCount != 2 {
		t.Errorf("ModifyCount = %d, 期望 2 (modified.txt 与 raw.bin)", summary.ModifyCount)
	}
	if summary.SameCount != 1 {
		t.Errorf("SameCount = %d, 期望 1 (common.txt)", summary.SameCount)
	}
	if len(summary.Files) != 4 {
		t.Errorf("Diff Files 数量 = %d, 期望 4", len(summary.Files))
	}

	// 1.1 测试 GetSameFiles 分页获取相同文件
	sameFiles, totalSame, err := service.GetSameFiles("", 0, 10)
	if err != nil {
		t.Fatalf("GetSameFiles 失败: %v", err)
	}
	if totalSame != 1 || len(sameFiles) != 1 {
		t.Errorf("GetSameFiles 返回数量不匹配: total=%d, len=%d", totalSame, len(sameFiles))
	}

	// 2. 测试获取单个文件的 Diff
	diff, err := service.GetDiff("script/modified.txt")
	if err != nil {
		t.Fatalf("GetDiff 失败: %v", err)
	}
	if diff.Operation != CompareOpModify {
		t.Errorf("Operation = %s, 期望 %s", diff.Operation, CompareOpModify)
	}
	if !diff.TextAvailable {
		t.Errorf("TextAvailable 期望 true")
	}
	if len(diff.Diff) == 0 {
		t.Errorf("Diff 行数期望 > 0")
	}

	// 2.1 测试单端独有文件的 Diff (新增与删除)
	diffAdded, err := service.GetDiff("script/added.txt")
	if err != nil || diffAdded.Operation != CompareOpAdd {
		t.Fatalf("GetDiff added.txt 失败: op=%v, err=%v", diffAdded.Operation, err)
	}
	diffDeleted, err := service.GetDiff("script/to_delete.txt")
	if err != nil || diffDeleted.Operation != CompareOpDelete {
		t.Fatalf("GetDiff to_delete.txt 失败: op=%v, err=%v", diffDeleted.Operation, err)
	}

	// 3. 测试 MergeFiles 合并功能 (包括脚本、非脚本二进制文件以及删除文件)
	mergeRes, err := service.MergeFiles([]string{"script/added.txt", "script/modified.txt", "bin/raw.bin", "script/to_delete.txt"})
	if err != nil {
		t.Fatalf("MergeFiles 失败: %v", err)
	}
	if mergeRes.SuccessCount != 4 {
		t.Errorf("MergeFiles SuccessCount = %d, 期望 4; 错误: %v", mergeRes.SuccessCount, mergeRes.Errors)
	}

	// 验证本地归档确实被更新并新增了 added.txt，raw.bin 二进制被替换，to_delete.txt 被删除
	core.mu.RLock()
	currentArchive := core.archive
	core.mu.RUnlock()
	if idx, exists := currentArchive.Find("script/added.txt"); !exists {
		t.Errorf("合并后未在本地找到 script/added.txt")
	} else {
		txt, _ := currentArchive.Text(idx)
		if !strings.Contains(txt, "`new_file`") {
			t.Errorf("script/added.txt 内容不匹配: %q", txt)
		}
	}
	if rawIdx, exists := currentArchive.Find("bin/raw.bin"); !exists {
		t.Errorf("合并后未在本地找到 bin/raw.bin")
	} else {
		raw, _ := currentArchive.RawBytes(rawIdx)
		if len(raw) != 4 || raw[0] != 9 {
			t.Errorf("bin/raw.bin 内容未被更新: %v", raw)
		}
	}
	if _, exists := currentArchive.Find("script/to_delete.txt"); exists {
		t.Errorf("script/to_delete.txt 未被删除")
	}

	// 4. 测试 MergeSingleFileText
	customText := "[name]\n`custom_merged`\n"
	if err := service.MergeSingleFileText("script/modified.txt", customText); err != nil {
		t.Fatalf("MergeSingleFileText 失败: %v", err)
	}
	if idx, exists := currentArchive.Find("script/modified.txt"); !exists {
		t.Errorf("script/modified.txt 不存在")
	} else {
		txt, _ := currentArchive.Text(idx)
		if !strings.Contains(txt, "`custom_merged`") {
			t.Errorf("script/modified.txt 自定义内容不匹配: %q", txt)
		}
	}

	// 5. 测试 CloseCompare
	if err := service.CloseCompare(); err != nil {
		t.Fatalf("CloseCompare 失败: %v", err)
	}
	if _, err := service.GetDiff("script/modified.txt"); err == nil {
		t.Errorf("关闭会话后 GetDiff 期望返回错误")
	}
}
