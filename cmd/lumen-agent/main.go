package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"lumen-agent/internal/agent"
	"lumen-agent/internal/auditlog"
	"lumen-agent/internal/config"
	"lumen-agent/internal/dashboard"
	"lumen-agent/internal/discordbot"
	"lumen-agent/internal/eventwebhook"
	"lumen-agent/internal/httpaux"
	"lumen-agent/internal/llm"
	"lumen-agent/internal/sandbox"
	"lumen-agent/internal/tools"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	command := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command = args[0]
		args = args[1:]
	}

	switch command {
	case "serve":
		return runServe(args)
	case "system-event":
		return runSystemEvent(args)
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown command %q\n\n%s", command, usageText())
	}
}

func runServe(args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	configPath := flags.String("config", "config/lumen.yaml", "Path to the Lumen Agent YAML config")
	workspaceDir := flags.String("workspace-dir", "", "Override the workspace directory available to tools and memory loading")
	if err := flags.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	overrideWorkspaceDir := strings.TrimSpace(*workspaceDir)
	if overrideWorkspaceDir == "" {
		overrideWorkspaceDir = strings.TrimSpace(os.Getenv("LUMEN_WORKSPACE_DIR"))
	}
	if err := cfg.OverrideWorkspaceRoot(overrideWorkspaceDir); err != nil {
		return fmt.Errorf("apply workspace override: %w", err)
	}

	if err := os.MkdirAll(cfg.App.SessionDir, 0o755); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}

	if err := os.MkdirAll(cfg.App.MemoryDir, 0o755); err != nil {
		return fmt.Errorf("create memory dir: %w", err)
	}

	alog, err := auditlog.New(cfg.LogDir())
	if err != nil {
		return fmt.Errorf("open audit log: %w", err)
	}
	defer alog.Close()

	apiKey, err := cfg.ResolveAPIKey()
	if err != nil {
		return fmt.Errorf("resolve API key: %w", err)
	}

	registry, err := tools.NewRegistry(cfg)
	if err != nil {
		return fmt.Errorf("initialize tools: %w", err)
	}
	defer func() {
		if closeErr := registry.Close(); closeErr != nil {
			alog.Write("error", "", map[string]any{"op": "close_tools_registry", "error": closeErr.Error()})
		}
	}()

	client := llm.NewClient(cfg.LLM.BaseURL, apiKey, cfg.LLM.APIType, cfg.LLM.Headers, cfg.LLMTimeout())
	runner := agent.NewRunner(cfg, client, registry)
	var sandboxManager tools.SandboxManager
	if cfg.BackgroundTasks.Sandbox.Enabled {
		sandboxManager = sandbox.NewManager(cfg)
		runner.SetSandboxManager(sandboxManager)
	}
	service, err := discordbot.New(cfg, runner, alog, sandboxManager)
	if err != nil {
		return fmt.Errorf("initialize Discord service: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	workers := 1
	errCh := make(chan error, 2)

	log.Printf(
		"startup: config=%s dashboard.enabled=%t dashboard.listen_addr=%q dashboard.path=%q",
		cfg.SourcePath(),
		cfg.Dashboard.Enabled,
		cfg.Dashboard.ListenAddr,
		cfg.Dashboard.Path,
	)

	go func() {
		errCh <- service.Run(ctx)
	}()

	if httpaux.CanShareListener(cfg) {
		workers++
		go func() {
			errCh <- httpaux.Run(ctx, cfg, alog)
		}()
	} else {
		if cfg.EventWebhook.Enabled {
			workers++
			go func() {
				errCh <- eventwebhook.Run(ctx, cfg, alog)
			}()
		}

		if cfg.Dashboard.Enabled {
			workers++
			go func() {
				errCh <- dashboard.Run(ctx, cfg)
			}()
		}
	}

	var firstErr error
	for i := 0; i < workers; i++ {
		err := <-errCh
		if err != nil && firstErr == nil {
			firstErr = err
			stop()
		}
	}

	return firstErr
}

func runSystemEvent(args []string) error {
	flags := flag.NewFlagSet("system-event", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	configPath := flags.String("config", "config/lumen.yaml", "Path to the Lumen Agent YAML config")
	text := flags.String("text", "", "System event text to queue for heartbeat")
	mode := flags.String("mode", "now", "Delivery mode: now or next-heartbeat")
	if err := flags.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if err := discordbot.EnqueueSystemEvent(cfg, *text, *mode); err != nil {
		return fmt.Errorf("queue system event: %w", err)
	}

	return nil
}

func printUsage() {
	fmt.Fprint(os.Stdout, usageText())
}

func usageText() string {
	return "Lumen Agent\n\nUsage:\n  lumen-agent [serve] [-config path] [-workspace-dir path]\n  lumen-agent system-event -text \"Check urgent follow-ups\" [-mode now|next-heartbeat] [-config path]\n  lumen-agent help\n\nEnvironment:\n  LUMEN_WORKSPACE_DIR   Override the workspace directory at runtime\n\nCommands:\n  serve         Run the Discord bot service (default)\n  system-event  Queue a heartbeat system event for the running service\n  help          Show this help text\n"
}
