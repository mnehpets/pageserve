package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"flag"

	"github.com/joho/godotenv"
	"github.com/mnehpets/pageserve"
)

var (
	addr    = flag.String("addr", "", "override the first listener address (e.g. \":9090\")")
	secrets = flag.String("secrets", "", "path to secrets.env file (default: secrets.env alongside config)")
)

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: pageserve [flags] <config.yaml>")
		flag.PrintDefaults()
		os.Exit(1)
	}
	configPath := args[0]

	secretsPath := *secrets
	if secretsPath == "" {
		absConfig, err := filepath.Abs(configPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "pageserve:", err)
			os.Exit(1)
		}
		secretsPath = filepath.Join(filepath.Dir(absConfig), "secrets.env")
	}

	env, err := mergedEnv(secretsPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pageserve:", err)
		os.Exit(1)
	}

	cfg, err := pageserve.Load(configPath, pageserve.WithEnv(env))
	if err != nil {
		fmt.Fprintln(os.Stderr, "pageserve:", err)
		os.Exit(1)
	}

	if *addr != "" {
		cfg.Server.Listeners[0].Address = *addr
	}

	srv, err := pageserve.Build(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pageserve:", err)
		os.Exit(1)
	}

	for _, l := range cfg.Server.Listeners {
		if l.TLS != nil {
			log.Printf("pageserve listening on %s (https)", l.Address)
		} else {
			log.Printf("pageserve listening on %s (http)", l.Address)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := pageserve.Serve(ctx, cfg, srv); err != nil {
		fmt.Fprintln(os.Stderr, "pageserve:", err)
		os.Exit(2)
	}
}

// mergedEnv loads secretsPath (if it exists) then overlays OS environment.
// OS env wins on conflict, per D3.
func mergedEnv(secretsPath string) (map[string]string, error) {
	env, err := godotenv.Read(secretsPath)
	if os.IsNotExist(err) {
		env = make(map[string]string)
	} else if err != nil {
		return nil, fmt.Errorf("read secrets file %q: %w", secretsPath, err)
	}
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	return env, nil
}
