package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	App             AppConfig             `yaml:"app"`
	LLM             LLMConfig             `yaml:"llm"`
	Tools           ToolsConfig           `yaml:"tools"`
	BackgroundTasks BackgroundTasksConfig `yaml:"background_tasks"`
	Discord         DiscordConfig         `yaml:"discord"`
	GIFs            GIFConfig             `yaml:"gifs"`
	Heartbeat       HeartbeatConfig       `yaml:"heartbeat"`
	EventWebhook    EventWebhookConfig    `yaml:"event_webhook"`
	Skills          SkillsConfig          `yaml:"skills"`
	MCP             MCPConfig             `yaml:"mcp"`

	sourcePath string `yaml:"-"`
}

func (c Config) SourcePath() string {
	return c.sourcePath
}

func (c *Config) SetSourcePath(path string) {
	c.sourcePath = strings.TrimSpace(path)
}

type AppConfig struct {
	Name                string                     `yaml:"name"`
	WorkspaceRoot       string                     `yaml:"workspace_root"`
	SessionDir          string                     `yaml:"session_dir"`
	MemoryDir           string                     `yaml:"memory_dir"`
	LoadAllMemoryShards bool                       `yaml:"load_all_memory_shards"`
	MaxAgentLoops       int                        `yaml:"max_agent_loops"`
	MaxToolCallsPerTurn int                        `yaml:"max_tool_calls_per_turn"`
	HistoryCompaction   AppHistoryCompactionConfig `yaml:"history_compaction"`
}

type AppHistoryCompactionConfig struct {
	Enabled                bool `yaml:"enabled"`
	TriggerTokens          int  `yaml:"trigger_tokens"`
	TargetTokens           int  `yaml:"target_tokens"`
	PreserveRecentMessages int  `yaml:"preserve_recent_messages"`
}

type LLMConfig struct {
	APIType                 string            `yaml:"api_type"`
	BaseURL                 string            `yaml:"base_url"`
	APIKey                  string            `yaml:"api_key"`
	APIKeyEnv               string            `yaml:"api_key_env"`
	Model                   string            `yaml:"model"`
	ReasoningEffort         string            `yaml:"reasoning_effort"`
	Temperature             float64           `yaml:"temperature"`
	MaxTokens               int               `yaml:"max_tokens"`
	ContextWindowTokens     int               `yaml:"context_window_tokens"`
	InjectMessageTimestamps bool              `yaml:"inject_message_timestamps"`
	Timeout                 string            `yaml:"timeout"`
	RequestMaxAttempts      int               `yaml:"request_max_attempts"`
	RetryInitialBackoff     string            `yaml:"retry_initial_backoff"`
	RetryMaxBackoff         string            `yaml:"retry_max_backoff"`
	Headers                 map[string]string `yaml:"headers"`
}

type SkillsConfig struct {
	Enabled bool             `yaml:"enabled"`
	Load    SkillsLoadConfig `yaml:"load"`
}

type SkillsLoadConfig struct {
	ExtraDirs  []string `yaml:"extra_dirs"`
	UserDir    string   `yaml:"user_dir"`
	BundledDir string   `yaml:"bundled_dir"`
}

type ToolsConfig struct {
	Enabled               []string `yaml:"enabled"`
	ExecShell             string   `yaml:"exec_shell"`
	ExecTimeout           string   `yaml:"exec_timeout"`
	MaxFileBytes          int64    `yaml:"max_file_bytes"`
	MaxSearchResults      int      `yaml:"max_search_results"`
	MaxCommandOutputBytes int      `yaml:"max_command_output_bytes"`
	AllowedCommands       []string `yaml:"allowed_commands"`
}

type BackgroundTasksConfig struct {
	DefaultMinRuntime  string                      `yaml:"default_min_runtime"`
	InjectCurrentTime  bool                        `yaml:"inject_current_time"`
	MaxEventLogEntries int                         `yaml:"max_event_log_entries"`
	Sandbox            BackgroundTaskSandboxConfig `yaml:"sandbox"`
}

type BackgroundTaskSandboxConfig struct {
	Enabled      bool   `yaml:"enabled"`
	Force        bool   `yaml:"force"`
	UseSudo      bool   `yaml:"use_sudo"`
	Provider     string `yaml:"provider"`
	Release      string `yaml:"release"`
	Architecture string `yaml:"architecture"`
	Mirror       string `yaml:"mirror"`
	MachinesDir  string `yaml:"machines_dir"`
	SetupTimeout string `yaml:"setup_timeout"`
	AutoCleanup  bool   `yaml:"auto_cleanup"`
}

type DiscordConfig struct {
	BotToken                    string   `yaml:"bot_token"`
	AllowDirectMessages         bool     `yaml:"allow_direct_messages"`
	AllowedGuildIDs             []string `yaml:"allowed_guild_ids"`
	AllowedDMUserIDs            []string `yaml:"allowed_dm_user_ids"`
	AllowedOutboundChannelIDs   []string `yaml:"allowed_outbound_channel_ids"`
	GuildSessionScope           string   `yaml:"guild_session_scope"`
	ReplyToMessage              bool     `yaml:"reply_to_message"`
	DownloadIncomingAttachments bool     `yaml:"download_incoming_attachments"`
	IncomingAttachmentsDir      string   `yaml:"incoming_attachments_dir"`
}

type GIFConfig struct {
	Enabled       bool   `yaml:"enabled"`
	Provider      string `yaml:"provider"`
	APIKey        string `yaml:"api_key"`
	APIKeyEnv     string `yaml:"api_key_env"`
	SearchLimit   int    `yaml:"search_limit"`
	ContentFilter string `yaml:"content_filter"`
}

type HeartbeatConfig struct {
	Every             string                     `yaml:"every"`
	Model             string                     `yaml:"model"`
	LightContext      bool                       `yaml:"light_context"`
	IsolatedSession   bool                       `yaml:"isolated_session"`
	AckMaxChars       int                        `yaml:"ack_max_chars"`
	ShowOK            bool                       `yaml:"show_ok"`
	ShowAlerts        bool                       `yaml:"show_alerts"`
	UseIndicator      bool                       `yaml:"use_indicator"`
	EventPollInterval string                     `yaml:"event_poll_interval"`
	ActiveHours       HeartbeatActiveHoursConfig `yaml:"active_hours"`
	Target            HeartbeatTargetConfig      `yaml:"target"`
}

type HeartbeatActiveHoursConfig struct {
	Timezone string `yaml:"timezone"`
	Start    string `yaml:"start"`
	End      string `yaml:"end"`
}

type HeartbeatTargetConfig struct {
	GuildID   string `yaml:"guild_id"`
	ChannelID string `yaml:"channel_id"`
	UserID    string `yaml:"user_id"`
}

type EventWebhookConfig struct {
	Enabled     bool   `yaml:"enabled"`
	ListenAddr  string `yaml:"listen_addr"`
	Path        string `yaml:"path"`
	Secret      string `yaml:"secret"`
	SecretEnv   string `yaml:"secret_env"`
	DefaultMode string `yaml:"default_mode"`
}

type MCPConfig struct {
	Servers []MCPServerConfig `yaml:"servers"`
}

type MCPServerConfig struct {
	Name           string            `yaml:"name"`
	Enabled        bool              `yaml:"enabled"`
	Transport      string            `yaml:"transport"`
	Command        string            `yaml:"command"`
	Args           []string          `yaml:"args"`
	Endpoint       string            `yaml:"endpoint"`
	Env            map[string]string `yaml:"env"`
	WorkingDir     string            `yaml:"working_dir"`
	StartupTimeout string            `yaml:"startup_timeout"`
	ToolTimeout    string            `yaml:"tool_timeout"`
}

func Load(path string) (Config, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return Config{}, fmt.Errorf("resolve config path: %w", err)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return Config{}, fmt.Errorf("read config file: %w", err)
	}

	cfg := defaultConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("decode config YAML: %w", err)
	}

	cfg.sourcePath = absPath
	if err := cfg.resolvePaths(); err != nil {
		return Config{}, err
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func defaultConfig() Config {
	return Config{
		App: AppConfig{
			Name:                "Lumen Agent",
			WorkspaceRoot:       ".",
			SessionDir:          ".lumen",
			MemoryDir:           "",
			LoadAllMemoryShards: false,
			MaxAgentLoops:       12,
			MaxToolCallsPerTurn: 24,
			HistoryCompaction: AppHistoryCompactionConfig{
				Enabled:                true,
				PreserveRecentMessages: 12,
			},
		},
		LLM: LLMConfig{
			APIType:                 "openai",
			BaseURL:                 "https://api.openai.com/v1",
			APIKeyEnv:               "OPENAI_API_KEY",
			Model:                   "gpt-4.1-mini",
			ReasoningEffort:         "",
			Temperature:             0.2,
			MaxTokens:               4000,
			ContextWindowTokens:     24000,
			InjectMessageTimestamps: true,
			Timeout:                 "180s",
			RequestMaxAttempts:      3,
			RetryInitialBackoff:     "2s",
			RetryMaxBackoff:         "8s",
			Headers:                 map[string]string{},
		},
		Tools: ToolsConfig{
			Enabled: []string{
				"send_discord_message",
				"add_discord_reaction",
				"discord_api_request",
				"read_file",
				"write_file",
				"replace_in_file",
				"list_dir",
				"grep_search",
				"mkdir",
				"move_path",
				"delete_path",
				"exec_command",
				"compact_context",
				"send_discord_file",
				"start_background_task",
				"list_background_tasks",
				"get_background_task",
				"get_background_task_logs",
				"cancel_background_task",
				"schedule_heartbeat_wakeup",
				"list_sandbox_containers",
				"inspect_sandbox_container",
				"create_sandbox_container",
				"start_sandbox_container",
				"stop_sandbox_container",
				"delete_sandbox_container",
			},
			ExecShell:             "/bin/zsh",
			ExecTimeout:           "120s",
			MaxFileBytes:          1 << 20,
			MaxSearchResults:      50,
			MaxCommandOutputBytes: 64 << 10,
			AllowedCommands:       []string{},
		},
		BackgroundTasks: BackgroundTasksConfig{
			DefaultMinRuntime:  "",
			InjectCurrentTime:  true,
			MaxEventLogEntries: 200,
			Sandbox: BackgroundTaskSandboxConfig{
				Enabled:      false,
				Force:        false,
				UseSudo:      false,
				Provider:     "nspawn",
				Release:      "stable",
				Architecture: "",
				Mirror:       "http://deb.debian.org/debian/",
				MachinesDir:  "",
				SetupTimeout: "20m",
				AutoCleanup:  true,
			},
		},
		Discord: DiscordConfig{
			BotToken:                    "",
			AllowDirectMessages:         false,
			AllowedGuildIDs:             []string{},
			AllowedDMUserIDs:            []string{},
			AllowedOutboundChannelIDs:   []string{},
			GuildSessionScope:           "channel",
			ReplyToMessage:              true,
			DownloadIncomingAttachments: true,
		},
		GIFs: GIFConfig{
			Enabled:       false,
			Provider:      "giphy",
			APIKeyEnv:     "GIPHY_API_KEY",
			SearchLimit:   8,
			ContentFilter: "pg-13",
		},
		Heartbeat: HeartbeatConfig{
			Every:             "30m",
			Model:             "",
			LightContext:      false,
			IsolatedSession:   true,
			AckMaxChars:       300,
			ShowOK:            false,
			ShowAlerts:        true,
			UseIndicator:      true,
			EventPollInterval: "5s",
			ActiveHours:       HeartbeatActiveHoursConfig{},
			Target:            HeartbeatTargetConfig{},
		},
		EventWebhook: EventWebhookConfig{
			Enabled:     false,
			ListenAddr:  "127.0.0.1:8787",
			Path:        "/event",
			Secret:      "",
			SecretEnv:   "LUMEN_EVENT_WEBHOOK_SECRET",
			DefaultMode: "now",
		},
		Skills: SkillsConfig{
			Enabled: true,
			Load: SkillsLoadConfig{
				ExtraDirs:  []string{},
				UserDir:    "~/.openclaw/skills",
				BundledDir: "../skills/bundled",
			},
		},
		MCP: MCPConfig{
			Servers: []MCPServerConfig{},
		},
	}
}

func (c *Config) resolvePaths() error {
	configDir := filepath.Dir(c.sourcePath)

	workspaceRoot, err := absFromBase(configDir, c.App.WorkspaceRoot)
	if err != nil {
		return fmt.Errorf("resolve app.workspace_root: %w", err)
	}
	c.App.WorkspaceRoot = workspaceRoot

	sessionDir, err := absFromBase(configDir, c.App.SessionDir)
	if err != nil {
		return fmt.Errorf("resolve app.session_dir: %w", err)
	}
	c.App.SessionDir = sessionDir

	c.App.MemoryDir = strings.TrimSpace(c.App.MemoryDir)
	if c.App.MemoryDir == "" {
		c.App.MemoryDir = filepath.Join(c.App.SessionDir, "memory")
	}
	memoryDir, err := absFromBase(configDir, c.App.MemoryDir)
	if err != nil {
		return fmt.Errorf("resolve app.memory_dir: %w", err)
	}
	c.App.MemoryDir = memoryDir

	if strings.TrimSpace(c.App.Name) == "" {
		c.App.Name = "Lumen Agent"
	}

	if c.App.MaxToolCallsPerTurn <= 0 {
		c.App.MaxToolCallsPerTurn = 24
	}

	if strings.TrimSpace(c.Tools.ExecShell) == "" {
		c.Tools.ExecShell = "/bin/zsh"
	}
	c.BackgroundTasks.DefaultMinRuntime = strings.TrimSpace(c.BackgroundTasks.DefaultMinRuntime)
	if c.BackgroundTasks.MaxEventLogEntries <= 0 {
		c.BackgroundTasks.MaxEventLogEntries = 200
	}
	if c.BackgroundTasks.Sandbox.Force {
		c.BackgroundTasks.Sandbox.Enabled = true
	}
	c.BackgroundTasks.Sandbox.Provider = strings.TrimSpace(strings.ToLower(c.BackgroundTasks.Sandbox.Provider))
	if c.BackgroundTasks.Sandbox.Provider == "" {
		c.BackgroundTasks.Sandbox.Provider = "nspawn"
	}
	c.BackgroundTasks.Sandbox.Release = strings.TrimSpace(c.BackgroundTasks.Sandbox.Release)
	if c.BackgroundTasks.Sandbox.Release == "" {
		c.BackgroundTasks.Sandbox.Release = "stable"
	}
	c.BackgroundTasks.Sandbox.Architecture = strings.TrimSpace(strings.ToLower(c.BackgroundTasks.Sandbox.Architecture))
	c.BackgroundTasks.Sandbox.Mirror = strings.TrimSpace(c.BackgroundTasks.Sandbox.Mirror)
	if c.BackgroundTasks.Sandbox.Mirror == "" {
		c.BackgroundTasks.Sandbox.Mirror = "http://deb.debian.org/debian/"
	}
	c.BackgroundTasks.Sandbox.MachinesDir = strings.TrimSpace(c.BackgroundTasks.Sandbox.MachinesDir)
	if c.BackgroundTasks.Sandbox.MachinesDir == "" {
		c.BackgroundTasks.Sandbox.MachinesDir = filepath.Join(c.App.SessionDir, "sandboxes")
	}
	resolvedMachinesDir, err := absFromBase(configDir, c.BackgroundTasks.Sandbox.MachinesDir)
	if err != nil {
		return fmt.Errorf("resolve background_tasks.sandbox.machines_dir: %w", err)
	}
	c.BackgroundTasks.Sandbox.MachinesDir = resolvedMachinesDir
	c.BackgroundTasks.Sandbox.SetupTimeout = strings.TrimSpace(c.BackgroundTasks.Sandbox.SetupTimeout)
	if c.BackgroundTasks.Sandbox.SetupTimeout == "" {
		c.BackgroundTasks.Sandbox.SetupTimeout = "20m"
	}
	c.Discord.IncomingAttachmentsDir = strings.TrimSpace(c.Discord.IncomingAttachmentsDir)
	if c.Discord.IncomingAttachmentsDir == "" {
		c.Discord.IncomingAttachmentsDir = filepath.Join(c.App.SessionDir, "incoming-attachments")
	}
	resolvedAttachmentsDir, err := absFromBase(configDir, c.Discord.IncomingAttachmentsDir)
	if err != nil {
		return fmt.Errorf("resolve discord.incoming_attachments_dir: %w", err)
	}
	c.Discord.IncomingAttachmentsDir = resolvedAttachmentsDir

	trimmedGuilds := make([]string, 0, len(c.Discord.AllowedGuildIDs))
	seenGuilds := make(map[string]struct{}, len(c.Discord.AllowedGuildIDs))
	for _, guildID := range c.Discord.AllowedGuildIDs {
		guildID = strings.TrimSpace(guildID)
		if guildID == "" {
			continue
		}
		if _, ok := seenGuilds[guildID]; ok {
			continue
		}
		seenGuilds[guildID] = struct{}{}
		trimmedGuilds = append(trimmedGuilds, guildID)
	}
	c.LLM.APIType = strings.TrimSpace(strings.ToLower(c.LLM.APIType))
	if c.LLM.APIType == "" {
		c.LLM.APIType = "openai"
	}
	c.LLM.APIKey = strings.TrimSpace(c.LLM.APIKey)
	if c.LLM.ContextWindowTokens <= 0 {
		c.LLM.ContextWindowTokens = 24000
	}
	if c.LLM.RequestMaxAttempts <= 0 {
		c.LLM.RequestMaxAttempts = 3
	}
	c.LLM.RetryInitialBackoff = strings.TrimSpace(c.LLM.RetryInitialBackoff)
	if c.LLM.RetryInitialBackoff == "" {
		c.LLM.RetryInitialBackoff = "2s"
	}
	c.LLM.RetryMaxBackoff = strings.TrimSpace(c.LLM.RetryMaxBackoff)
	if c.LLM.RetryMaxBackoff == "" {
		c.LLM.RetryMaxBackoff = "8s"
	}
	c.Discord.BotToken = strings.TrimSpace(c.Discord.BotToken)
	c.Discord.AllowedGuildIDs = trimmedGuilds
	c.Discord.AllowedDMUserIDs = uniqueTrimmedStrings(c.Discord.AllowedDMUserIDs)
	c.Discord.AllowedOutboundChannelIDs = uniqueTrimmedStrings(c.Discord.AllowedOutboundChannelIDs)
	c.Discord.GuildSessionScope = strings.TrimSpace(strings.ToLower(c.Discord.GuildSessionScope))
	if c.Discord.GuildSessionScope == "" {
		c.Discord.GuildSessionScope = "channel"
	}
	c.GIFs.Provider = strings.TrimSpace(strings.ToLower(c.GIFs.Provider))
	if c.GIFs.Provider == "" {
		c.GIFs.Provider = "giphy"
	}
	c.GIFs.APIKey = strings.TrimSpace(c.GIFs.APIKey)
	c.GIFs.APIKeyEnv = strings.TrimSpace(c.GIFs.APIKeyEnv)
	if c.GIFs.APIKeyEnv == "" {
		c.GIFs.APIKeyEnv = "GIPHY_API_KEY"
	}
	if c.GIFs.SearchLimit <= 0 {
		c.GIFs.SearchLimit = 8
	}
	c.GIFs.ContentFilter = strings.TrimSpace(strings.ToLower(c.GIFs.ContentFilter))
	switch c.GIFs.ContentFilter {
	case "off":
		c.GIFs.ContentFilter = "r"
	case "low":
		c.GIFs.ContentFilter = "g"
	case "medium":
		c.GIFs.ContentFilter = "pg"
	case "high":
		c.GIFs.ContentFilter = "pg-13"
	}
	if c.GIFs.ContentFilter == "" {
		c.GIFs.ContentFilter = "pg-13"
	}

	c.Heartbeat.Every = strings.TrimSpace(c.Heartbeat.Every)
	c.Heartbeat.Model = strings.TrimSpace(c.Heartbeat.Model)
	if c.Heartbeat.AckMaxChars <= 0 {
		c.Heartbeat.AckMaxChars = 300
	}
	if strings.TrimSpace(c.Heartbeat.EventPollInterval) == "" {
		c.Heartbeat.EventPollInterval = "5s"
	}
	c.Heartbeat.ActiveHours.Timezone = strings.TrimSpace(c.Heartbeat.ActiveHours.Timezone)
	c.Heartbeat.ActiveHours.Start = strings.TrimSpace(c.Heartbeat.ActiveHours.Start)
	c.Heartbeat.ActiveHours.End = strings.TrimSpace(c.Heartbeat.ActiveHours.End)
	c.Heartbeat.Target.GuildID = strings.TrimSpace(c.Heartbeat.Target.GuildID)
	c.Heartbeat.Target.ChannelID = strings.TrimSpace(c.Heartbeat.Target.ChannelID)
	c.Heartbeat.Target.UserID = strings.TrimSpace(c.Heartbeat.Target.UserID)

	c.EventWebhook.ListenAddr = strings.TrimSpace(c.EventWebhook.ListenAddr)
	c.EventWebhook.Path = strings.TrimSpace(c.EventWebhook.Path)
	c.EventWebhook.Secret = strings.TrimSpace(c.EventWebhook.Secret)
	c.EventWebhook.SecretEnv = strings.TrimSpace(c.EventWebhook.SecretEnv)
	c.EventWebhook.DefaultMode = strings.TrimSpace(strings.ToLower(c.EventWebhook.DefaultMode))
	if c.EventWebhook.ListenAddr == "" {
		c.EventWebhook.ListenAddr = "127.0.0.1:8787"
	}
	if c.EventWebhook.Path == "" {
		c.EventWebhook.Path = "/event"
	}
	if c.EventWebhook.SecretEnv == "" {
		c.EventWebhook.SecretEnv = "LUMEN_EVENT_WEBHOOK_SECRET"
	}
	if c.EventWebhook.DefaultMode == "" {
		c.EventWebhook.DefaultMode = "now"
	}

	if strings.TrimSpace(c.Skills.Load.UserDir) == "" {
		c.Skills.Load.UserDir = "~/.openclaw/skills"
	}
	resolvedSkillsUserDir, err := absOrHomeFromBase(configDir, c.Skills.Load.UserDir)
	if err != nil {
		return fmt.Errorf("resolve skills.load.user_dir: %w", err)
	}
	c.Skills.Load.UserDir = resolvedSkillsUserDir

	if strings.TrimSpace(c.Skills.Load.BundledDir) == "" {
		c.Skills.Load.BundledDir = "../skills/bundled"
	}
	resolvedBundledSkillsDir, err := absOrHomeFromBase(configDir, c.Skills.Load.BundledDir)
	if err != nil {
		return fmt.Errorf("resolve skills.load.bundled_dir: %w", err)
	}
	c.Skills.Load.BundledDir = resolvedBundledSkillsDir

	extraSkillDirs := make([]string, 0, len(c.Skills.Load.ExtraDirs))
	seenSkillDirs := make(map[string]struct{}, len(c.Skills.Load.ExtraDirs))
	for _, extraDir := range c.Skills.Load.ExtraDirs {
		resolvedExtraDir, err := absOrHomeFromBase(configDir, extraDir)
		if err != nil {
			return fmt.Errorf("resolve skills.load.extra_dirs entry %q: %w", extraDir, err)
		}
		if resolvedExtraDir == "" {
			continue
		}
		if _, seen := seenSkillDirs[resolvedExtraDir]; seen {
			continue
		}
		seenSkillDirs[resolvedExtraDir] = struct{}{}
		extraSkillDirs = append(extraSkillDirs, resolvedExtraDir)
	}
	c.Skills.Load.ExtraDirs = extraSkillDirs

	servers := make([]MCPServerConfig, 0, len(c.MCP.Servers))
	for _, server := range c.MCP.Servers {
		server.Name = strings.TrimSpace(server.Name)
		server.Transport = strings.TrimSpace(strings.ToLower(server.Transport))
		server.Command = strings.TrimSpace(server.Command)
		server.Endpoint = strings.TrimSpace(server.Endpoint)
		server.WorkingDir = strings.TrimSpace(server.WorkingDir)
		server.StartupTimeout = strings.TrimSpace(server.StartupTimeout)
		server.ToolTimeout = strings.TrimSpace(server.ToolTimeout)
		if server.Env == nil {
			server.Env = map[string]string{}
		}
		if server.Transport == "" {
			server.Transport = "stdio"
		}
		if server.StartupTimeout == "" {
			server.StartupTimeout = "30s"
		}
		if server.ToolTimeout == "" {
			server.ToolTimeout = "120s"
		}
		if server.WorkingDir != "" {
			resolved, err := absFromBase(configDir, server.WorkingDir)
			if err != nil {
				return fmt.Errorf("resolve mcp.servers[%s].working_dir: %w", server.Name, err)
			}
			server.WorkingDir = resolved
		}
		servers = append(servers, server)
	}
	c.MCP.Servers = servers
	c.LLM.ReasoningEffort = strings.TrimSpace(strings.ToLower(c.LLM.ReasoningEffort))

	return nil
}

func (c Config) validate() error {
	if !slices.Contains([]string{"openai", "codex"}, c.LLM.APIType) {
		return fmt.Errorf("llm.api_type must be one of openai or codex")
	}

	if strings.TrimSpace(c.LLM.BaseURL) == "" {
		return fmt.Errorf("llm.base_url must be set")
	}

	if strings.TrimSpace(c.LLM.Model) == "" {
		return fmt.Errorf("llm.model must be set")
	}
	if c.LLM.ReasoningEffort != "" && !slices.Contains([]string{"none", "minimal", "low", "medium", "high", "xhigh"}, c.LLM.ReasoningEffort) {
		return fmt.Errorf("llm.reasoning_effort must be one of none, minimal, low, medium, high, or xhigh")
	}

	if c.App.MaxAgentLoops <= 0 {
		return fmt.Errorf("app.max_agent_loops must be greater than zero")
	}

	if c.App.MaxToolCallsPerTurn <= 0 {
		return fmt.Errorf("app.max_tool_calls_per_turn must be greater than zero")
	}

	if c.Tools.MaxFileBytes <= 0 {
		return fmt.Errorf("tools.max_file_bytes must be greater than zero")
	}

	if c.Tools.MaxSearchResults <= 0 {
		return fmt.Errorf("tools.max_search_results must be greater than zero")
	}

	if c.Tools.MaxCommandOutputBytes <= 0 {
		return fmt.Errorf("tools.max_command_output_bytes must be greater than zero")
	}

	if c.App.HistoryCompaction.Enabled {
		if c.App.HistoryCompaction.TriggerTokens < 0 {
			return fmt.Errorf("app.history_compaction.trigger_tokens must not be negative")
		}
		if c.App.HistoryCompaction.TargetTokens < 0 {
			return fmt.Errorf("app.history_compaction.target_tokens must not be negative")
		}
		if c.App.HistoryCompaction.PreserveRecentMessages < 0 {
			return fmt.Errorf("app.history_compaction.preserve_recent_messages must not be negative")
		}
		if c.App.HistoryCompaction.TriggerTokens > 0 && c.App.HistoryCompaction.TargetTokens > 0 &&
			c.App.HistoryCompaction.TargetTokens >= c.App.HistoryCompaction.TriggerTokens {
			return fmt.Errorf("app.history_compaction.target_tokens must be smaller than app.history_compaction.trigger_tokens")
		}
	}

	if c.Discord.BotToken == "" {
		return fmt.Errorf("discord.bot_token must be set")
	}

	if !c.Discord.AllowDirectMessages && len(c.Discord.AllowedGuildIDs) == 0 {
		return fmt.Errorf("configure at least one discord.allowed_guild_ids entry or enable discord.allow_direct_messages")
	}

	if !slices.Contains([]string{"channel", "user"}, c.Discord.GuildSessionScope) {
		return fmt.Errorf("discord.guild_session_scope must be one of channel or user")
	}
	if err := validateOptionalDirectoryPath(c.Discord.IncomingAttachmentsDir, "discord.incoming_attachments_dir"); err != nil {
		return err
	}
	if !slices.Contains([]string{"giphy"}, c.GIFs.Provider) {
		return fmt.Errorf("gifs.provider must be one of giphy")
	}
	if c.GIFs.SearchLimit <= 0 {
		return fmt.Errorf("gifs.search_limit must be greater than zero")
	}
	if !slices.Contains([]string{"g", "pg", "pg-13", "r"}, c.GIFs.ContentFilter) {
		return fmt.Errorf("gifs.content_filter must be one of g, pg, pg-13, or r")
	}

	if err := validateDirectoryPath(c.App.WorkspaceRoot, "app.workspace_root"); err != nil {
		return err
	}

	if err := validateOptionalDirectoryPath(c.App.MemoryDir, "app.memory_dir"); err != nil {
		return err
	}

	if _, err := time.ParseDuration(c.LLM.Timeout); err != nil {
		return fmt.Errorf("parse llm.timeout: %w", err)
	}
	if c.LLM.RequestMaxAttempts <= 0 {
		return fmt.Errorf("llm.request_max_attempts must be greater than zero")
	}
	if _, err := time.ParseDuration(c.LLM.RetryInitialBackoff); err != nil {
		return fmt.Errorf("parse llm.retry_initial_backoff: %w", err)
	}
	if _, err := time.ParseDuration(c.LLM.RetryMaxBackoff); err != nil {
		return fmt.Errorf("parse llm.retry_max_backoff: %w", err)
	}

	if c.LLM.ContextWindowTokens <= 0 {
		return fmt.Errorf("llm.context_window_tokens must be greater than zero")
	}

	if c.LLM.MaxTokens <= 0 {
		return fmt.Errorf("llm.max_tokens must be greater than zero")
	}

	if c.LLM.ContextWindowTokens <= c.LLM.MaxTokens {
		return fmt.Errorf("llm.context_window_tokens must be greater than llm.max_tokens")
	}

	if _, err := time.ParseDuration(c.Tools.ExecTimeout); err != nil {
		return fmt.Errorf("parse tools.exec_timeout: %w", err)
	}
	if strings.TrimSpace(c.BackgroundTasks.DefaultMinRuntime) != "" {
		if _, err := time.ParseDuration(c.BackgroundTasks.DefaultMinRuntime); err != nil {
			return fmt.Errorf("parse background_tasks.default_min_runtime: %w", err)
		}
	}
	if c.BackgroundTasks.MaxEventLogEntries <= 0 {
		return fmt.Errorf("background_tasks.max_event_log_entries must be greater than zero")
	}
	if c.BackgroundTasks.Sandbox.Enabled {
		if !slices.Contains([]string{"nspawn"}, c.BackgroundTasks.Sandbox.Provider) {
			return fmt.Errorf("background_tasks.sandbox.provider must be one of nspawn")
		}
		if err := validateDirectoryPath(c.BackgroundTasks.Sandbox.MachinesDir, "background_tasks.sandbox.machines_dir"); err != nil {
			return err
		}
		if _, err := time.ParseDuration(c.BackgroundTasks.Sandbox.SetupTimeout); err != nil {
			return fmt.Errorf("parse background_tasks.sandbox.setup_timeout: %w", err)
		}
	}

	if strings.TrimSpace(c.Heartbeat.Every) != "" {
		if _, err := time.ParseDuration(c.Heartbeat.Every); err != nil {
			return fmt.Errorf("parse heartbeat.every: %w", err)
		}
	}

	if _, err := time.ParseDuration(c.Heartbeat.EventPollInterval); err != nil {
		return fmt.Errorf("parse heartbeat.event_poll_interval: %w", err)
	}

	if c.Heartbeat.AckMaxChars <= 0 {
		return fmt.Errorf("heartbeat.ack_max_chars must be greater than zero")
	}

	if (c.Heartbeat.ActiveHours.Start == "") != (c.Heartbeat.ActiveHours.End == "") {
		return fmt.Errorf("heartbeat.active_hours.start and heartbeat.active_hours.end must be set together")
	}
	if c.Heartbeat.ActiveHours.Start != "" {
		if _, err := parseClockHHMM(c.Heartbeat.ActiveHours.Start); err != nil {
			return fmt.Errorf("parse heartbeat.active_hours.start: %w", err)
		}
		if _, err := parseClockHHMM(c.Heartbeat.ActiveHours.End); err != nil {
			return fmt.Errorf("parse heartbeat.active_hours.end: %w", err)
		}
		if c.Heartbeat.ActiveHours.Timezone != "" {
			if _, err := time.LoadLocation(c.Heartbeat.ActiveHours.Timezone); err != nil {
				return fmt.Errorf("load heartbeat.active_hours.timezone: %w", err)
			}
		}
	}

	if c.EventWebhook.Enabled {
		if strings.TrimSpace(c.EventWebhook.ListenAddr) == "" {
			return fmt.Errorf("event_webhook.listen_addr must be set when event_webhook.enabled is true")
		}
		if !strings.HasPrefix(c.EventWebhook.Path, "/") {
			return fmt.Errorf("event_webhook.path must start with '/'")
		}
		if !isValidHeartbeatMode(c.EventWebhook.DefaultMode) {
			return fmt.Errorf("event_webhook.default_mode must be one of now or next-heartbeat")
		}
		if !c.HeartbeatEnabled() {
			return fmt.Errorf("event_webhook.enabled requires heartbeat target and schedule configuration")
		}
	}

	for index, server := range c.MCP.Servers {
		if !server.Enabled {
			continue
		}
		label := fmt.Sprintf("mcp.servers[%d]", index)
		if server.Name == "" {
			return fmt.Errorf("%s.name must be set when the server is enabled", label)
		}
		switch server.Transport {
		case "stdio":
			if server.Command == "" {
				return fmt.Errorf("%s.command must be set for stdio transport", label)
			}
		case "http", "streamable_http":
			if server.Endpoint == "" {
				return fmt.Errorf("%s.endpoint must be set for HTTP transport", label)
			}
		default:
			return fmt.Errorf("%s.transport must be one of stdio, http, or streamable_http", label)
		}
		if server.WorkingDir != "" {
			if err := validateDirectoryPath(server.WorkingDir, label+".working_dir"); err != nil {
				return err
			}
		}
		if _, err := time.ParseDuration(server.StartupTimeout); err != nil {
			return fmt.Errorf("parse %s.startup_timeout: %w", label, err)
		}
		if _, err := time.ParseDuration(server.ToolTimeout); err != nil {
			return fmt.Errorf("parse %s.tool_timeout: %w", label, err)
		}
	}

	if err := validateOptionalDirectoryPath(c.Skills.Load.UserDir, "skills.load.user_dir"); err != nil {
		return err
	}

	if err := validateOptionalDirectoryPath(c.Skills.Load.BundledDir, "skills.load.bundled_dir"); err != nil {
		return err
	}

	for index, extraDir := range c.Skills.Load.ExtraDirs {
		field := fmt.Sprintf("skills.load.extra_dirs[%d]", index)
		if err := validateOptionalDirectoryPath(extraDir, field); err != nil {
			return err
		}
	}

	return nil
}

func (c Config) ResolveAPIKey() (string, error) {
	if c.LLM.APIKey != "" {
		return c.LLM.APIKey, nil
	}

	envName := strings.TrimSpace(c.LLM.APIKeyEnv)
	if envName == "" {
		return "", fmt.Errorf("set llm.api_key in config or llm.api_key_env pointing to an environment variable")
	}

	value := strings.TrimSpace(os.Getenv(envName))
	if value == "" {
		return "", fmt.Errorf("environment variable %q is empty; set llm.api_key or export the variable", envName)
	}

	return value, nil
}

func (c Config) ResolveEventWebhookSecret() (string, error) {
	if strings.TrimSpace(c.EventWebhook.Secret) != "" {
		return c.EventWebhook.Secret, nil
	}

	envName := strings.TrimSpace(c.EventWebhook.SecretEnv)
	if envName == "" {
		return "", nil
	}

	return strings.TrimSpace(os.Getenv(envName)), nil
}

func (c Config) ResolveGIFAPIKey() (string, error) {
	if strings.TrimSpace(c.GIFs.APIKey) != "" {
		return strings.TrimSpace(c.GIFs.APIKey), nil
	}

	envName := strings.TrimSpace(c.GIFs.APIKeyEnv)
	if envName == "" {
		return "", fmt.Errorf("set gifs.api_key in config or gifs.api_key_env pointing to an environment variable")
	}

	value := strings.TrimSpace(os.Getenv(envName))
	if value == "" {
		return "", fmt.Errorf("environment variable %q is empty; set gifs.api_key or export the variable", envName)
	}

	return value, nil
}

func (c Config) LLMTimeout() time.Duration {
	timeout, err := time.ParseDuration(c.LLM.Timeout)
	if err != nil || timeout <= 0 {
		return 180 * time.Second
	}
	return timeout
}

func (c Config) LLMRetryInitialBackoff() time.Duration {
	backoff, err := time.ParseDuration(c.LLM.RetryInitialBackoff)
	if err != nil || backoff <= 0 {
		return 2 * time.Second
	}
	return backoff
}

func (c Config) LLMRetryMaxBackoff() time.Duration {
	backoff, err := time.ParseDuration(c.LLM.RetryMaxBackoff)
	if err != nil || backoff <= 0 {
		return 8 * time.Second
	}
	return backoff
}

func (c Config) LLMInputTokenBudget() int {
	if c.LLM.ContextWindowTokens <= 0 {
		return 0
	}

	budget := c.LLM.ContextWindowTokens - c.LLM.MaxTokens
	if budget <= 0 {
		return c.LLM.ContextWindowTokens
	}

	return budget
}

func (c Config) HistoryCompactionTriggerTokens() int {
	if !c.App.HistoryCompaction.Enabled {
		return 0
	}
	if c.App.HistoryCompaction.TriggerTokens > 0 {
		return c.App.HistoryCompaction.TriggerTokens
	}
	budget := c.LLMInputTokenBudget()
	if budget <= 0 {
		return 0
	}
	trigger := (budget * 3) / 4
	if trigger <= 0 {
		return budget
	}
	return trigger
}

func (c Config) HistoryCompactionTargetTokens() int {
	if !c.App.HistoryCompaction.Enabled {
		return 0
	}
	if c.App.HistoryCompaction.TargetTokens > 0 {
		return c.App.HistoryCompaction.TargetTokens
	}
	trigger := c.HistoryCompactionTriggerTokens()
	if trigger <= 0 {
		return 0
	}
	target := (trigger * 2) / 3
	if target <= 0 {
		return trigger / 2
	}
	return target
}

func (c Config) HistoryCompactionPreserveRecentMessages() int {
	if !c.App.HistoryCompaction.Enabled {
		return 0
	}
	if c.App.HistoryCompaction.PreserveRecentMessages > 0 {
		return c.App.HistoryCompaction.PreserveRecentMessages
	}
	return 12
}

func (c Config) ExecTimeout() time.Duration {
	timeout, err := time.ParseDuration(c.Tools.ExecTimeout)
	if err != nil || timeout <= 0 {
		return 120 * time.Second
	}
	return timeout
}

func (c Config) BackgroundTaskDefaultMinRuntime() time.Duration {
	value := strings.TrimSpace(c.BackgroundTasks.DefaultMinRuntime)
	if value == "" {
		return 0
	}
	runtime, err := time.ParseDuration(value)
	if err != nil || runtime <= 0 {
		return 0
	}
	return runtime
}

func (c Config) BackgroundTaskMaxEventLogEntries() int {
	if c.BackgroundTasks.MaxEventLogEntries > 0 {
		return c.BackgroundTasks.MaxEventLogEntries
	}
	return 200
}

func (c Config) BackgroundTaskSandboxSetupTimeout() time.Duration {
	timeout, err := time.ParseDuration(c.BackgroundTasks.Sandbox.SetupTimeout)
	if err != nil || timeout <= 0 {
		return 20 * time.Minute
	}
	return timeout
}

func (c Config) HeartbeatInterval() time.Duration {
	interval, err := time.ParseDuration(c.Heartbeat.Every)
	if err != nil || interval <= 0 {
		return 30 * time.Minute
	}
	return interval
}

func (c Config) HeartbeatEventPollInterval() time.Duration {
	interval, err := time.ParseDuration(c.Heartbeat.EventPollInterval)
	if err != nil || interval <= 0 {
		return 5 * time.Second
	}
	return interval
}

func (c Config) HeartbeatModel() string {
	if strings.TrimSpace(c.Heartbeat.Model) != "" {
		return c.Heartbeat.Model
	}
	return c.LLM.Model
}

func (c Config) HeartbeatEventsDir() string {
	return filepath.Join(c.App.SessionDir, "heartbeat-events")
}

func (c Config) CronJobsDir() string {
	return filepath.Join(c.App.SessionDir, "cron-jobs")
}

func (c Config) LogDir() string {
	return filepath.Join(c.App.SessionDir, "logs")
}

func (c Config) MCPServerStartupTimeout(server MCPServerConfig) time.Duration {
	timeout, err := time.ParseDuration(server.StartupTimeout)
	if err != nil || timeout <= 0 {
		return 30 * time.Second
	}
	return timeout
}

func (c Config) MCPServerToolTimeout(server MCPServerConfig) time.Duration {
	timeout, err := time.ParseDuration(server.ToolTimeout)
	if err != nil || timeout <= 0 {
		return 120 * time.Second
	}
	return timeout
}

func (c Config) HeartbeatLocation() (*time.Location, error) {
	if strings.TrimSpace(c.Heartbeat.ActiveHours.Timezone) == "" {
		return time.Local, nil
	}
	return time.LoadLocation(c.Heartbeat.ActiveHours.Timezone)
}

func (c Config) HeartbeatEnabled() bool {
	return strings.TrimSpace(c.Heartbeat.Every) != "" &&
		strings.TrimSpace(c.Heartbeat.Target.ChannelID) != "" &&
		strings.TrimSpace(c.Heartbeat.Target.UserID) != ""
}

func (c Config) HeartbeatHasAnyDelivery() bool {
	return c.Heartbeat.ShowOK || c.Heartbeat.ShowAlerts || c.Heartbeat.UseIndicator
}

func (c Config) ToolEnabled(name string) bool {
	if len(c.Tools.Enabled) == 0 {
		return true
	}
	return slices.Contains(c.Tools.Enabled, name)
}

func (c Config) SharedGuildSessions() bool {
	return strings.EqualFold(strings.TrimSpace(c.Discord.GuildSessionScope), "channel")
}

func (c Config) DMAllowedForUser(userID string) bool {
	if !c.Discord.AllowDirectMessages {
		return false
	}

	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false
	}

	if len(c.Discord.AllowedDMUserIDs) == 0 {
		return true
	}

	return slices.Contains(c.Discord.AllowedDMUserIDs, userID)
}

func (c Config) DiscordChannelAllowed(targetChannelID string, activeChannelID string) bool {
	targetChannelID = strings.TrimSpace(targetChannelID)
	activeChannelID = strings.TrimSpace(activeChannelID)
	if targetChannelID == "" {
		return false
	}
	if activeChannelID != "" && targetChannelID == activeChannelID {
		return true
	}
	return slices.Contains(c.Discord.AllowedOutboundChannelIDs, targetChannelID)
}

func (c *Config) OverrideWorkspaceRoot(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}

	previousWorkspaceRoot := strings.TrimSpace(c.App.WorkspaceRoot)
	previousSessionDir := strings.TrimSpace(c.App.SessionDir)
	previousMemoryDir := strings.TrimSpace(c.App.MemoryDir)

	workingDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	resolved, err := absFromBase(workingDir, path)
	if err != nil {
		return fmt.Errorf("resolve workspace override: %w", err)
	}

	if err := validateDirectoryPath(resolved, "workspace override"); err != nil {
		return err
	}

	c.App.WorkspaceRoot = resolved
	if previousWorkspaceRoot != "" && sameCleanPath(previousSessionDir, filepath.Join(previousWorkspaceRoot, ".lumen")) {
		c.App.SessionDir = filepath.Join(resolved, ".lumen")
	}
	if previousMemoryDir == "" || sameCleanPath(previousMemoryDir, filepath.Join(previousSessionDir, "memory")) {
		c.App.MemoryDir = filepath.Join(c.App.SessionDir, "memory")
	}
	return nil
}

func sameCleanPath(left string, right string) bool {
	trimmedLeft := strings.TrimSpace(left)
	trimmedRight := strings.TrimSpace(right)
	if trimmedLeft == "" || trimmedRight == "" {
		return trimmedLeft == trimmedRight
	}
	return filepath.Clean(trimmedLeft) == filepath.Clean(trimmedRight)
}

func validateDirectoryPath(path string, fieldName string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", fieldName, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("%s must point to a directory", fieldName)
	}

	return nil
}

func validateOptionalDirectoryPath(path string, fieldName string) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil
	}

	info, err := os.Stat(trimmed)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", fieldName, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("%s must point to a directory when it exists", fieldName)
	}

	return nil
}

func parseClockHHMM(value string) (int, error) {
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		return 0, err
	}
	return parsed.Hour()*60 + parsed.Minute(), nil
}

func isValidHeartbeatMode(mode string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(mode))
	return trimmed == "now" || trimmed == "next-heartbeat"
}

func absFromBase(base string, candidate string) (string, error) {
	if strings.TrimSpace(candidate) == "" {
		candidate = "."
	}

	if filepath.IsAbs(candidate) {
		return filepath.Clean(candidate), nil
	}

	return filepath.Abs(filepath.Join(base, candidate))
}

func absOrHomeFromBase(base string, candidate string) (string, error) {
	trimmed := strings.TrimSpace(candidate)
	if trimmed == "" {
		return "", nil
	}

	expanded, err := expandHome(trimmed)
	if err != nil {
		return "", err
	}

	if filepath.IsAbs(expanded) {
		return filepath.Clean(expanded), nil
	}

	return filepath.Abs(filepath.Join(base, expanded))
}

func expandHome(path string) (string, error) {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		return home, nil
	}

	if !strings.HasPrefix(path, "~/") {
		return path, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
}

func uniqueTrimmedStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}
