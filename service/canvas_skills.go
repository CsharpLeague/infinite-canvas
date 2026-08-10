package service

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
)

func ListAdminCanvasSkills(q model.Query) (model.CanvasSkillList, error) {
	items, total, err := repository.ListCanvasSkills(q, false)
	return model.CanvasSkillList{Items: items, Total: int(total)}, err
}

func ListPublishedCanvasSkills(q model.Query) ([]model.CanvasSkillSummary, error) {
	items, _, err := repository.ListCanvasSkills(q, true)
	if err != nil {
		return nil, err
	}
	result := make([]model.CanvasSkillSummary, 0, len(items))
	for _, item := range items {
		result = append(result, model.CanvasSkillSummary{ID: item.ID, Slug: item.Slug, Name: item.Name, Description: item.Description, Category: item.Category, Icon: item.Icon, Placeholder: item.Placeholder})
	}
	return result, nil
}

func ImportCanvasSkillPackage(data []byte) (model.CanvasSkill, error) {
	if len(data) == 0 || len(data) > 10<<20 {
		return model.CanvasSkill{}, errors.New("Skill ZIP 必须小于 10MB")
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return model.CanvasSkill{}, errors.New("无法读取 Skill ZIP")
	}
	files := map[string]string{}
	skillPath := ""
	for _, file := range reader.File {
		name := strings.TrimPrefix(path.Clean(strings.ReplaceAll(file.Name, "\\", "/")), "./")
		if !file.FileInfo().IsDir() && !strings.HasPrefix(name, "../") && path.Base(name) == "SKILL.md" && (skillPath == "" || len(name) < len(skillPath)) {
			skillPath = name
		}
	}
	if skillPath == "" {
		return model.CanvasSkill{}, errors.New("ZIP 中缺少 SKILL.md")
	}
	root := strings.TrimSuffix(skillPath, "SKILL.md")
	var total int64
	for _, file := range reader.File {
		name := strings.TrimPrefix(path.Clean(strings.ReplaceAll(file.Name, "\\", "/")), "./")
		if file.FileInfo().IsDir() || !strings.HasPrefix(name, root) {
			continue
		}
		relative := strings.TrimPrefix(name, root)
		if relative != "SKILL.md" && !strings.HasPrefix(relative, "references/") {
			continue
		}
		if file.UncompressedSize64 > 1<<20 {
			return model.CanvasSkill{}, fmt.Errorf("Skill 文件过大：%s", relative)
		}
		opened, err := file.Open()
		if err != nil {
			return model.CanvasSkill{}, err
		}
		content, err := io.ReadAll(io.LimitReader(opened, 1<<20+1))
		_ = opened.Close()
		if err != nil {
			return model.CanvasSkill{}, err
		}
		total += int64(len(content))
		if total > 3<<20 {
			return model.CanvasSkill{}, errors.New("Skill 文本内容总计不能超过 3MB")
		}
		files[relative] = string(content)
	}
	metadata, instructions, err := parseSkillMarkdown(files["SKILL.md"])
	if err != nil {
		return model.CanvasSkill{}, err
	}
	item := model.CanvasSkill{Slug: metadata["name"], Name: metadata["name"], Description: metadata["description"], Category: "通用", Instructions: instructions, Files: files, Status: model.CanvasSkillStatusDraft}
	if saved, findErr := repository.FindCanvasSkill(item.Slug, false); findErr == nil {
		item.ID = saved.ID
		item.CreatedAt = saved.CreatedAt
		item.AllowedTools = saved.AllowedTools
	}
	return SaveCanvasSkill(item)
}

func parseSkillMarkdown(content string) (map[string]string, string, error) {
	content = strings.TrimPrefix(content, "\ufeff")
	parts := strings.SplitN(content, "---", 3)
	if len(parts) != 3 || strings.TrimSpace(parts[0]) != "" {
		return nil, "", errors.New("SKILL.md 缺少 YAML 元数据")
	}
	metadata := map[string]string{}
	for _, line := range strings.Split(parts[1], "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok {
			metadata[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), "\"'")
		}
	}
	if metadata["name"] == "" || metadata["description"] == "" {
		return nil, "", errors.New("SKILL.md 的 name 和 description 不能为空")
	}
	return metadata, strings.TrimSpace(parts[2]), nil
}

func SaveCanvasSkill(item model.CanvasSkill) (model.CanvasSkill, error) {
	item.Name = strings.TrimSpace(item.Name)
	item.Slug = strings.ToLower(strings.TrimSpace(item.Slug))
	item.Category = strings.TrimSpace(item.Category)
	item.Instructions = strings.TrimSpace(item.Instructions)
	if item.Name == "" || item.Slug == "" || item.Instructions == "" {
		return item, errors.New("Skill 名称、标识和运行指令不能为空")
	}
	if item.Status != model.CanvasSkillStatusPublished {
		item.Status = model.CanvasSkillStatusDraft
	}
	if item.Category == "" {
		item.Category = "通用"
	}
	item.AllowedTools = uniqueStrings(item.AllowedTools)
	now := time.Now().Format(time.RFC3339)
	if item.ID == "" {
		item.ID = newID("skill")
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	return repository.SaveCanvasSkill(item)
}

func DeleteCanvasSkill(id string) error {
	return repository.DeleteCanvasSkill(id)
}

func uniqueStrings(items []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" && !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}
