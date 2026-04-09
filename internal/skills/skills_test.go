package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"element-orion/internal/config"
)

func TestLoaderPrecedenceWorkspaceOverridesOthers(t *testing.T) {
	root := t.TempDir()
	bundled := filepath.Join(root, "bundled")
	userDir := filepath.Join(root, "user")
	workspace := filepath.Join(root, "workspace")
	workspaceSkills := filepath.Join(workspace, "skills")

	writeSkill(t, filepath.Join(bundled, "deploy", "SKILL.md"), `---
name: deploy-check
description: bundled description
---
Bundled`)
	writeSkill(t, filepath.Join(userDir, "deploy", "SKILL.md"), `---
name: deploy-check
description: user description
---
User`)
	writeSkill(t, filepath.Join(workspaceSkills, "deploy", "SKILL.md"), `---
name: deploy-check
description: workspace description
---
Workspace`)

	cfg := config.Config{
		App: config.AppConfig{WorkspaceRoot: workspace},
		Skills: config.SkillsConfig{
			Enabled: true,
			Load: config.SkillsLoadConfig{
				BundledDir: bundled,
				UserDir:    userDir,
			},
		},
	}

	snapshot := NewLoader(cfg).Snapshot()
	if len(snapshot) != 1 {
		t.Fatalf("expected one skill, got %d", len(snapshot))
	}
	if snapshot[0].Description != "workspace description" {
		t.Fatalf("expected workspace skill to override lower precedence entries, got %q", snapshot[0].Description)
	}
	if snapshot[0].Source != "workspace" {
		t.Fatalf("expected workspace source, got %q", snapshot[0].Source)
	}
}

func TestLoaderIncludesClaudeCodeWorkspaceSkillsAndCommands(t *testing.T) {
	workspace := t.TempDir()
	writeSkill(t, filepath.Join(workspace, ".claude", "skills", "release", "SKILL.md"), `---
name: release-skill
description: claude workspace skill
version: 1.2.3
---
content`)
	writeSkill(t, filepath.Join(workspace, ".claude", "commands", "triage.md"), `---
description: claude workspace command
---
content`)
	writeSkill(t, filepath.Join(workspace, ".claude", "commands", "ops", "deploy.md"), `---
description: deploy command
---
content`)

	cfg := config.Config{
		App:    config.AppConfig{WorkspaceRoot: workspace},
		Skills: config.SkillsConfig{Enabled: true},
	}

	snapshot := NewLoader(cfg).Snapshot()
	if len(snapshot) != 3 {
		t.Fatalf("expected three Claude-compatible entries, got %d", len(snapshot))
	}

	byName := map[string]Summary{}
	for _, skill := range snapshot {
		byName[skill.Name] = skill
	}

	if byName["release-skill"].Description != "claude workspace skill" {
		t.Fatalf("expected Claude workspace skill to load, got %#v", byName["release-skill"])
	}
	if byName["release-skill"].Version != "1.2.3" {
		t.Fatalf("expected Claude workspace skill version to be preserved, got %#v", byName["release-skill"])
	}
	if byName["triage"].Description != "claude workspace command" {
		t.Fatalf("expected Claude command fallback name to load, got %#v", byName["triage"])
	}
	if byName["ops/deploy"].Description != "deploy command" {
		t.Fatalf("expected nested Claude command fallback name to use relative path, got %#v", byName["ops/deploy"])
	}
}

func TestLoaderPrefersWorkspaceSkillsOverClaudeWorkspaceSkills(t *testing.T) {
	workspace := t.TempDir()
	writeSkill(t, filepath.Join(workspace, ".claude", "skills", "review", "SKILL.md"), `---
name: review
description: claude description
---
Claude`)
	writeSkill(t, filepath.Join(workspace, "skills", "review", "SKILL.md"), `---
name: review
description: workspace description
---
Workspace`)

	cfg := config.Config{
		App:    config.AppConfig{WorkspaceRoot: workspace},
		Skills: config.SkillsConfig{Enabled: true},
	}

	snapshot := NewLoader(cfg).Snapshot()
	if len(snapshot) != 1 {
		t.Fatalf("expected one merged skill, got %d", len(snapshot))
	}
	if snapshot[0].Description != "workspace description" {
		t.Fatalf("expected native workspace skills to keep highest precedence, got %q", snapshot[0].Description)
	}
}

func TestLoaderIncludesClaudeUserSkills(t *testing.T) {
	workspace := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	writeSkill(t, filepath.Join(homeDir, ".claude", "skills", "incident", "SKILL.md"), `---
name: incident
description: claude user skill
---
content`)

	cfg := config.Config{
		App:    config.AppConfig{WorkspaceRoot: workspace},
		Skills: config.SkillsConfig{Enabled: true},
	}

	snapshot := NewLoader(cfg).Snapshot()
	if len(snapshot) != 1 {
		t.Fatalf("expected one Claude user skill, got %d", len(snapshot))
	}
	if snapshot[0].Name != "incident" || snapshot[0].Description != "claude user skill" {
		t.Fatalf("expected Claude user skill to load, got %#v", snapshot[0])
	}
}

func TestLoaderSkipsSkillsWithMissingRequirements(t *testing.T) {
	workspace := t.TempDir()
	writeSkill(t, filepath.Join(workspace, "skills", "requires-env", "SKILL.md"), `---
name: secret-skill
description: needs env
metadata:
  openclaw:
    requires:
      env:
        - DEFINITELY_MISSING_ENV_FOR_TEST
---
content`)

	cfg := config.Config{
		App:    config.AppConfig{WorkspaceRoot: workspace},
		Skills: config.SkillsConfig{Enabled: true},
	}

	snapshot := NewLoader(cfg).Snapshot()
	if len(snapshot) != 0 {
		t.Fatalf("expected missing-requirement skill to be filtered, got %d", len(snapshot))
	}
}

func TestRenderPromptXMLEscapesValues(t *testing.T) {
	xml := RenderPromptXML([]Summary{{
		Name:        `a<skill>&`,
		Description: `desc "quoted"`,
		Location:    `/tmp/skills/<x>`,
		Version:     `1.0.0`,
	}})

	for _, expected := range []string{
		"<skills>",
		"name=\"a&lt;skill&gt;&amp;\"",
		"description=\"desc &quot;quoted&quot;\"",
		"location=\"/tmp/skills/&lt;x&gt;\"",
		"version=\"1.0.0\"",
		"</skills>",
	} {
		if !strings.Contains(xml, expected) {
			t.Fatalf("expected xml to contain %q", expected)
		}
	}
}

func writeSkill(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
}
