package skills

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"element-orion/internal/config"
)

const skillDocName = "SKILL.md"

type Summary struct {
	Name        string
	Description string
	Version     string
	Location    string
	Source      string
}

type Loader struct {
	cfg config.Config
}

type sourceDir struct {
	Path   string
	Source string
}

type frontmatter struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Version     string         `yaml:"version"`
	Metadata    map[string]any `yaml:"metadata"`
}

type requirements struct {
	Env        []string
	Bins       []string
	PrimaryEnv string
}

func NewLoader(cfg config.Config) *Loader {
	return &Loader{cfg: cfg}
}

// Snapshot loads eligible skills and returns a precedence-resolved, stable list.
func (l *Loader) Snapshot() []Summary {
	if !l.cfg.Skills.Enabled {
		return nil
	}

	byName := make(map[string]Summary)
	for _, source := range l.skillSourceDirs() {
		skills, err := loadSkillsFromDir(source.Path, source.Source)
		if err != nil {
			continue
		}
		for _, skill := range skills {
			if strings.TrimSpace(skill.Name) == "" {
				continue
			}
			byName[strings.ToLower(skill.Name)] = skill
		}
	}

	if len(byName) == 0 {
		return nil
	}

	result := make([]Summary, 0, len(byName))
	for _, skill := range byName {
		result = append(result, skill)
	}

	sort.Slice(result, func(i, j int) bool {
		if !strings.EqualFold(result[i].Name, result[j].Name) {
			return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
		}
		return strings.ToLower(result[i].Location) < strings.ToLower(result[j].Location)
	})

	return result
}

func (l *Loader) skillSourceDirs() []sourceDir {
	workspaceSkillsDir := filepath.Join(l.cfg.App.WorkspaceRoot, "skills")

	dirs := []sourceDir{}
	if strings.TrimSpace(l.cfg.Skills.Load.BundledDir) != "" {
		dirs = append(dirs, sourceDir{Path: l.cfg.Skills.Load.BundledDir, Source: "bundled"})
	}
	if strings.TrimSpace(l.cfg.Skills.Load.UserDir) != "" {
		dirs = append(dirs, sourceDir{Path: l.cfg.Skills.Load.UserDir, Source: "user"})
	}
	for _, extraDir := range l.cfg.Skills.Load.ExtraDirs {
		if strings.TrimSpace(extraDir) == "" {
			continue
		}
		dirs = append(dirs, sourceDir{Path: extraDir, Source: "extra"})
	}
	dirs = append(dirs, sourceDir{Path: workspaceSkillsDir, Source: "workspace"})

	return dirs
}

func loadSkillsFromDir(root string, source string) ([]Summary, error) {
	if strings.TrimSpace(root) == "" {
		return nil, nil
	}

	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}

	skills := make([]Summary, 0, 8)
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}

		if entry.IsDir() {
			return nil
		}
		if !strings.EqualFold(entry.Name(), skillDocName) {
			return nil
		}

		skill, eligible, err := parseSkill(path, source)
		if err != nil || !eligible {
			return nil
		}
		skills = append(skills, skill)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	sort.Slice(skills, func(i, j int) bool {
		if !strings.EqualFold(skills[i].Name, skills[j].Name) {
			return strings.ToLower(skills[i].Name) < strings.ToLower(skills[j].Name)
		}
		return strings.ToLower(skills[i].Location) < strings.ToLower(skills[j].Location)
	})

	return skills, nil
}

func parseSkill(path string, source string) (Summary, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Summary{}, false, err
	}

	frontmatterText, body := splitFrontmatter(string(data))
	meta := frontmatter{}
	if strings.TrimSpace(frontmatterText) != "" {
		if err := yaml.Unmarshal([]byte(frontmatterText), &meta); err != nil {
			return Summary{}, false, err
		}
	}

	req := parseRequirements(meta.Metadata)
	if !requirementsSatisfied(req) {
		return Summary{}, false, nil
	}

	name := strings.TrimSpace(meta.Name)
	if name == "" {
		name = strings.TrimSpace(filepath.Base(filepath.Dir(path)))
	}
	if name == "" {
		name = "unnamed-skill"
	}

	description := strings.TrimSpace(meta.Description)
	if description == "" {
		description = deriveDescription(body)
	}
	if description == "" {
		description = "No description provided."
	}

	return Summary{
		Name:        name,
		Description: description,
		Version:     strings.TrimSpace(meta.Version),
		Location:    filepath.ToSlash(filepath.Dir(path)),
		Source:      source,
	}, true, nil
}

func splitFrontmatter(content string) (string, string) {
	lines := strings.Split(content, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return "", strings.TrimSpace(content)
	}

	for index := 1; index < len(lines); index++ {
		if strings.TrimSpace(lines[index]) != "---" {
			continue
		}
		front := strings.Join(lines[1:index], "\n")
		body := strings.Join(lines[index+1:], "\n")
		return front, strings.TrimSpace(body)
	}

	return "", strings.TrimSpace(content)
}

func deriveDescription(body string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		return trimmed
	}
	return ""
}

func parseRequirements(metadata map[string]any) requirements {
	if len(metadata) == 0 {
		return requirements{}
	}

	openclawMetadata := metadataSubtree(metadata, "openclaw", "clawdbot", "clawdis")
	if len(openclawMetadata) == 0 {
		return requirements{}
	}

	requiresData := mapValue(openclawMetadata, "requires")
	req := requirements{
		Env:        uniqueStrings(listValue(requiresData, "env")),
		Bins:       uniqueStrings(listValue(requiresData, "bins")),
		PrimaryEnv: strings.TrimSpace(stringValue(openclawMetadata, "primaryEnv")),
	}

	if req.PrimaryEnv != "" && !containsIgnoreCase(req.Env, req.PrimaryEnv) {
		req.Env = append(req.Env, req.PrimaryEnv)
	}

	return req
}

func requirementsSatisfied(req requirements) bool {
	for _, envName := range req.Env {
		if strings.TrimSpace(os.Getenv(envName)) == "" {
			return false
		}
	}

	for _, binName := range req.Bins {
		if _, err := exec.LookPath(binName); err != nil {
			return false
		}
	}

	return true
}

func metadataSubtree(root map[string]any, aliases ...string) map[string]any {
	for _, alias := range aliases {
		child := mapValue(root, alias)
		if len(child) > 0 {
			return child
		}
	}
	return nil
}

func mapValue(root map[string]any, key string) map[string]any {
	if len(root) == 0 {
		return nil
	}

	for k, value := range root {
		if !strings.EqualFold(strings.TrimSpace(k), strings.TrimSpace(key)) {
			continue
		}
		if child, ok := toStringMap(value); ok {
			return child
		}
	}
	return nil
}

func listValue(root map[string]any, key string) []string {
	if len(root) == 0 {
		return nil
	}

	for k, value := range root {
		if !strings.EqualFold(strings.TrimSpace(k), strings.TrimSpace(key)) {
			continue
		}
		return toStringSlice(value)
	}
	return nil
}

func stringValue(root map[string]any, key string) string {
	if len(root) == 0 {
		return ""
	}
	for k, value := range root {
		if !strings.EqualFold(strings.TrimSpace(k), strings.TrimSpace(key)) {
			continue
		}
		s, ok := value.(string)
		if !ok {
			return ""
		}
		return s
	}
	return ""
}

func toStringMap(value any) (map[string]any, bool) {
	if value == nil {
		return nil, false
	}

	if mapped, ok := value.(map[string]any); ok {
		return mapped, true
	}

	if mapped, ok := value.(map[any]any); ok {
		result := make(map[string]any, len(mapped))
		for k, v := range mapped {
			key, ok := k.(string)
			if !ok {
				continue
			}
			result[key] = v
		}
		return result, true
	}

	return nil, false
}

func toStringSlice(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		if typed, ok := value.([]string); ok {
			return uniqueStrings(typed)
		}
		return nil
	}

	result := make([]string, 0, len(raw))
	for _, item := range raw {
		text, ok := item.(string)
		if !ok {
			continue
		}
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}
	return uniqueStrings(result)
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func containsIgnoreCase(values []string, candidate string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}

func RenderPromptXML(skills []Summary) string {
	if len(skills) == 0 {
		return ""
	}

	sorted := make([]Summary, len(skills))
	copy(sorted, skills)
	sort.Slice(sorted, func(i, j int) bool {
		if !strings.EqualFold(sorted[i].Name, sorted[j].Name) {
			return strings.ToLower(sorted[i].Name) < strings.ToLower(sorted[j].Name)
		}
		return strings.ToLower(sorted[i].Location) < strings.ToLower(sorted[j].Location)
	})

	var builder strings.Builder
	builder.WriteString("<skills>")
	for _, skill := range sorted {
		builder.WriteString("\n  <skill")
		builder.WriteString(" name=\"")
		builder.WriteString(escapeXML(skill.Name))
		builder.WriteString("\"")
		builder.WriteString(" description=\"")
		builder.WriteString(escapeXML(skill.Description))
		builder.WriteString("\"")
		builder.WriteString(" location=\"")
		builder.WriteString(escapeXML(skill.Location))
		builder.WriteString("\"")
		if strings.TrimSpace(skill.Version) != "" {
			builder.WriteString(" version=\"")
			builder.WriteString(escapeXML(skill.Version))
			builder.WriteString("\"")
		}
		builder.WriteString(" />")
	}
	builder.WriteString("\n</skills>")
	return builder.String()
}

func escapeXML(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&apos;",
	)
	return replacer.Replace(value)
}
