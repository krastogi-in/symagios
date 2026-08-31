package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	agententry "symagios/agent-entry"
	agentgateway "symagios/agent-gateway"
	"symagios/cli"
	"symagios/config"
	"symagios/contentguard"
	"symagios/encrypt"
	"symagios/engine"
	"symagios/engine/hooks"
	"symagios/llm"
	applog "symagios/log"
	"symagios/storage/sqlite"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "engine":
		if err := runEngine(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "engine error: %v\n", err)
			os.Exit(1)
		}
	case "watch":
		if err := runWatch(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "watch error: %v\n", err)
			os.Exit(1)
		}
	case "agent-entry":
		if err := runAgentEntry(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "agent-entry error: %v\n", err)
			os.Exit(1)
		}
	case "agent-gateway":
		if err := runAgentGateway(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "agent-gateway error: %v\n", err)
			os.Exit(1)
		}
	default:
		printUsage()
		os.Exit(1)
	}
}

func runEngine(args []string) error {
	cfg := config.EngineFromEnv()
	fs := flag.NewFlagSet("engine", flag.ExitOnError)
	configPath := fs.String("config", config.DefaultLLMConfigPath(), "path to deploy/llm/config.yaml")
	config.RegisterEngineFlags(fs, &cfg)
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	keyMaterial, err := encrypt.KeyFromVaultOrEnv(ctx)
	if err != nil {
		return err
	}
	encryptor, err := encrypt.NewEncryptKey(keyMaterial)
	if err != nil {
		return fmt.Errorf("create encryptor: %w", err)
	}

	hooks.ConfigureGuards(hooks.GuardConfig{
		Landlock:   cfg.GuardLandlock,
		Seccomp:    cfg.GuardSeccomp,
		AllowPaths: cfg.GuardAllowPaths,
	})
	mode := contentguard.ModeRedact
	if cfg.ContentGuardMode == "block" {
		mode = contentguard.ModeBlock
	}
	hooks.ConfigureContentGuards(contentguard.Config{
		Enabled:      cfg.ContentGuard,
		Mode:         mode,
		MaxRunes:     cfg.ContentMaxRunes,
		BlockedTerms: cfg.ContentBlockedTerms,
	})

	db, err := sqlite.New(sqlite.Config{Path: cfg.DBPath})
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	applog.Init(applog.Config{
		Level:  cfg.LogLevel,
		Format: cfg.LogFormat,
	})

	llmCfg, err := llm.ConfigFromFile(*configPath)
	if err != nil {
		applog.Default().With("component", "main").Info("llm config unavailable", "path", *configPath, "error", err)
	} else if c, err := llm.New(ctx, llmCfg); err != nil {
		applog.Default().With("component", "main").Info("llm client not configured", "error", err)
	} else {
		engine.ConfigureLLM(c)
		applog.Default().With("component", "main").Info("llm client configured", "provider", string(c.Provider()), "config", *configPath)
	}

	eng := engine.New(db, encryptor)
	eng.Start(ctx, engine.RunConfig{Workers: cfg.Workers})

	applog.Default().With("component", "main").Info(
		"engine running",
		"db", cfg.DBPath,
		"workers", cfg.Workers,
	)

	<-ctx.Done()
	eng.Stop()
	applog.Default().With("component", "main").Info("engine stopped")
	return nil
}

func runWatch(args []string) error {
	cfg := config.DefaultWatch()
	fs := flag.NewFlagSet("watch", flag.ExitOnError)
	config.RegisterWatchFlags(fs, &cfg)
	if err := fs.Parse(args); err != nil {
		return err
	}

	applog.Init(applog.Config{
		Level:  cfg.LogLevel,
		Format: cfg.LogFormat,
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	key, db, err := openKeyAndDB(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()

	engine.InitStore(db, key)

	return cli.Watch(ctx, db, key, cfg)
}

func runAgentEntry(args []string) error {
	cfg := agententry.ConfigFromEnv()
	fs := flag.NewFlagSet("agent-entry", flag.ExitOnError)
	dbPath := fs.String("db", envOr("SYMAGIOS_DB_PATH", "symagios.db"), "sqlite path")
	listen := fs.String("listen", cfg.Listen, "listen address")
	tlsCert := fs.String("tls-cert", cfg.TLSCertFile, "TLS cert (enables ListenAndServeTLS)")
	tlsKey := fs.String("tls-key", cfg.TLSKeyFile, "TLS key")
	tlsCA := fs.String("tls-ca", cfg.TLSCAFile, "CA to require client certs (mTLS)")
	logLevel := fs.String("log-level", envOr("SYMAGIOS_LOG_LEVEL", "info"), "log level")
	if err := fs.Parse(args); err != nil {
		return err
	}

	applog.Init(applog.Config{Level: *logLevel, Format: "json"})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	key, db, err := openKeyAndDB(ctx, *dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	srv := agententry.New(db, key)
	if err := srv.Start(ctx, agententry.Config{
		Listen:      *listen,
		TLSCertFile: *tlsCert,
		TLSKeyFile:  *tlsKey,
		TLSCAFile:   *tlsCA,
	}); err != nil {
		return err
	}
	<-ctx.Done()
	srv.Stop()
	return nil
}

func runAgentGateway(args []string) error {
	cfg := agentgateway.ConfigFromEnv()
	fs := flag.NewFlagSet("agent-gateway", flag.ExitOnError)
	dbPath := fs.String("db", envOr("SYMAGIOS_DB_PATH", "symagios.db"), "sqlite path")
	listen := fs.String("listen", envOr("SYMAGIOS_AGENT_GATEWAY_LISTEN", ":8443"), "listen address")
	agentName := fs.String("name", cfg.AgentName, "A2A agent name")
	version := fs.String("version", cfg.Version, "A2A agent version")
	publicURL := fs.String("url", cfg.URL, "public A2A URL (optional)")
	egress := fs.String("egress", cfg.EgressProxy, "egress proxy for external A2A")
	hostsCSV := fs.String("external-hosts", strings.Join(cfg.ExternalHosts, ","), "comma-separated external A2A hosts")
	tlsCert := fs.String("tls-cert", cfg.TLSCertFile, "TLS cert (enables ListenAndServeTLS)")
	tlsKey := fs.String("tls-key", cfg.TLSKeyFile, "TLS key")
	tlsCA := fs.String("tls-ca", cfg.TLSCAFile, "CA to require client certs (mTLS)")
	logLevel := fs.String("log-level", envOr("SYMAGIOS_LOG_LEVEL", "info"), "log level")
	if err := fs.Parse(args); err != nil {
		return err
	}

	applog.Init(applog.Config{Level: *logLevel, Format: "json"})

	host, port, err := splitListen(*listen)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	key, db, err := openKeyAndDB(ctx, *dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	gw := agentgateway.New(db, key, agentgateway.GatewayConfig{
		AgentName:     *agentName,
		Version:       *version,
		URL:           *publicURL,
		Host:          host,
		EgressProxy:   *egress,
		ExternalHosts: splitCSV(*hostsCSV),
		TLSCertFile:   *tlsCert,
		TLSKeyFile:    *tlsKey,
		TLSCAFile:     *tlsCA,
	})
	if err := gw.Start(ctx, host, port); err != nil {
		return err
	}
	<-ctx.Done()
	gw.Stop()
	return nil
}

func splitCSV(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func openKeyAndDB(ctx context.Context, dbPath string) (*encrypt.EncryptKey, *sqlite.Client, error) {
	keyMaterial, err := encrypt.KeyFromVaultOrEnv(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("encryption key: %w", err)
	}
	key, err := encrypt.NewEncryptKey(keyMaterial)
	if err != nil {
		return nil, nil, fmt.Errorf("encrypt key: %w", err)
	}
	db, err := sqlite.New(sqlite.Config{Path: dbPath})
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return key, db, nil
}

func splitListen(addr string) (host string, port int, err error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		// Allow ":8080"
		if strings.HasPrefix(addr, ":") {
			host = "0.0.0.0"
			portStr = strings.TrimPrefix(addr, ":")
		} else {
			return "", 0, fmt.Errorf("listen address: %w", err)
		}
	}
	if host == "" {
		host = "0.0.0.0"
	}
	port, err = strconv.Atoi(portStr)
	if err != nil {
		return "", 0, fmt.Errorf("listen port: %w", err)
	}
	return host, port, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `symagios - agent orchestration runtime

Usage:
  symagios engine        [--db PATH] [--workers N]
  symagios watch         [--db PATH] [--poll DURATION] [--session ID] [--max-preview N]
  symagios agent-entry   [--db PATH] [--listen ADDR]
  symagios agent-gateway [--db PATH] [--listen ADDR] [--name NAME]

Watch polls action_runs (status/result changes) and prompts for WAITING_FOR_HUMAN_INPUT.
  SYMAGIOS_WATCH_SESSION       optional session_id filter
  SYMAGIOS_WATCH_MAX_PREVIEW   result/payload preview runes (default 400)

Encryption key (engine / agent-entry / agent-gateway):
  Loaded from Vault secret/symagios/engine field encryption_key.
  Override for tests only: SYMAGIOS_ENCRYPTION_KEY (32 bytes).
  Vault: VAULT_ADDR VAULT_TOKEN VAULT_CACERT VAULT_CLIENT_CERT VAULT_CLIENT_KEY

Script/agent/skill guards (Landlock + seccomp in sandboxed children):
  SYMAGIOS_GUARD_LANDLOCK      true|false (default true)
  SYMAGIOS_GUARD_SECCOMP       true|false (default true)
  SYMAGIOS_GUARD_ALLOW_PATHS   comma paths (default /tmp/symagios-workspace)

LLM content guards (PII / secrets / injection on pre+post text):
  SYMAGIOS_CONTENT_GUARD         true|false (default true)
  SYMAGIOS_CONTENT_MODE          redact|block (default redact)
  SYMAGIOS_CONTENT_MAX_RUNES     max prompt/result size (default 200000)
  SYMAGIOS_CONTENT_BLOCKED_TERMS comma phrases to strip/block

Environment:
  SYMAGIOS_DB_PATH     SQLite database path (default: symagios.db)
  SYMAGIOS_WORKERS     Worker count for engine (default: 4)
  SYMAGIOS_LOG_LEVEL   debug|info|warn|error (default: info)
  SYMAGIOS_LOG_FORMAT  json|text (default: json)
`)
}
