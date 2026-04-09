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
