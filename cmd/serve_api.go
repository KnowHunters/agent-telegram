package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	"agent-telegram/internal/config"
	"agent-telegram/internal/gateway"
	"agent-telegram/internal/ipc"
	"agent-telegram/internal/paths"
	telegramipc "agent-telegram/internal/telegram/ipc"
	"agent-telegram/telegram"
	"agent-telegram/telegram/types"
)

var (
	serveAPIPort      int
	serveAPIListen    string
	serveAPISecret    string
	serveAPICORS      string
	serveAPIFileRoots []string
	serveAPIUnsafe    bool
	serveAPILogout    bool
	serveGatewayDB    string
)

var serveAPICmd = &cobra.Command{
	Use:     "serve-api",
	Aliases: []string{"gateway"},
	Short:   "Start HTTP REST API server with Telegram",
	Long: `Start an HTTP server that exposes all Telegram actions as REST endpoints.

Each registered method is available at POST /rpc/{method} with a JSON body.

Examples:
  curl -X POST http://localhost:8080/rpc/send_message \
    -H "Authorization: Bearer <secret>" \
    -H "Content-Type: application/json" \
    -d '{"peer": "username", "message": "hello"}'

  curl http://localhost:8080/health
  curl http://localhost:8080/methods -H "Authorization: Bearer <secret>"`,
	GroupID: GroupIDServer,
	RunE:    runServeAPI,
}

func init() {
	RootCmd.AddCommand(serveAPICmd)

	serveAPICmd.Flags().IntVar(&serveAPIPort, "port", 8080, "HTTP API server port")
	serveAPICmd.Flags().StringVar(&serveAPIListen, "listen", "127.0.0.1", "HTTP listen address")
	serveAPICmd.Flags().StringVar(&serveAPISecret, "secret", "",
		"Bearer token for auth (or AGENT_TELEGRAM_API_SECRET env)")
	serveAPICmd.Flags().StringVar(&serveAPICORS, "cors", "", "CORS allowed origins (comma-separated, empty disables CORS)")
	serveAPICmd.Flags().StringSliceVar(
		&serveAPIFileRoots,
		"file-root",
		nil,
		"Allow HTTP file parameters only below these server directories (repeatable)",
	)
	serveAPICmd.Flags().BoolVar(&serveAPIUnsafe, "unsafe-no-auth", false, "Allow HTTP API without auth")
	serveAPICmd.Flags().BoolVar(&serveAPILogout, "logout-on-stop", false, "Logout from Telegram when the server stops")
	serveAPICmd.Flags().StringVar(&serveGatewayDB, "gateway-db", "", "Enable authenticated WebSocket gateway and durable receipts using this SQLite path")
}

func runServeAPI(cmd *cobra.Command, _ []string) error {
	if cmd.CalledAs() == "gateway" && serveGatewayDB == "" {
		serveGatewayDB = DefaultGatewayPath()
	}
	storedCfg, err := config.LoadStoredConfig()
	if err != nil {
		return err
	}

	setupLogger()

	secret := serveAPISecret
	if secret == "" {
		secret = os.Getenv("AGENT_TELEGRAM_API_SECRET")
	}
	if secret == "" && !serveAPIUnsafe {
		return fmt.Errorf("--secret or AGENT_TELEGRAM_API_SECRET is required")
	}

	tgClient := createTelegramClient(storedCfg.AppID, storedCfg.AppHash, telegramClientOptions{
		Provider: firstConfigured(sessionFlagValue(cmd, "session-provider"), storedCfg.SessionProvider),
		Profile:  firstConfigured(sessionFlagValue(cmd, "profile"), storedCfg.SessionProfile),
	})
	logoutOnStop := boolFromEnv(envLogoutOnStop, serveAPILogout)
	var logoutOnce sync.Once
	logout := func() {
		logoutOnce.Do(func() {
			logoutTelegramClient(tgClient, logoutOnStop)
		})
	}
	ctx, cancel := setupContextWithShutdown(logout)
	defer cancel()
	var gatewayFailed atomic.Bool
	failGateway := func() { gatewayFailed.Store(true); cancel() }
	var journal *gateway.Store
	if serveGatewayDB != "" {
		if secret == "" {
			return fmt.Errorf("gateway requires authentication")
		}
		var err error
		configDir, err := paths.EnsureConfigDir()
		if err != nil {
			return fmt.Errorf("gateway lock directory: %w", err)
		}
		lock := paths.NewLockFile(filepath.Join(configDir, "gateway-"+tgClient.SessionStorageStatus().Profile+".lock"))
		locked, err := lock.TryLock()
		if err != nil || !locked {
			return fmt.Errorf("another gateway owns this session profile, or its lock is unavailable")
		}
		defer func() { _ = lock.Unlock() }()
		journal, err = gateway.Open(serveGatewayDB)
		if err != nil {
			return fmt.Errorf("open gateway journal: %w", err)
		}
		journalDone := startGatewayJournal(ctx, tgClient, journal, failGateway)
		defer func() { cancel(); tgClient.SetUpdateCallback(nil); <-journalDone; _ = journal.Close() }()
	}
	startTelegramClient(ctx, tgClient)
	go waitForTelegramReady(ctx, tgClient)

	address := net.JoinHostPort(serveAPIListen, strconv.Itoa(serveAPIPort))
	srv := createHTTPAPIServer(address, secret, serveAPICORS, serveAPIFileRoots, tgClient, cancel)
	srv.SetPolicyChecker(loadPolicyChecker(tgClient))
	if journal != nil {
		srv.EnableGateway(journal, func(ctx context.Context) string { return gatewayAccount(ctx, tgClient) })
		go gatewayStatusEvents(ctx, tgClient, journal, failGateway)
	}
	fmt.Fprintf(os.Stderr, "REST API on http://%s\n", address)

	if err := srv.Start(ctx); err != nil {
		return fmt.Errorf("HTTP server: %w", err)
	}

	logout()
	cancel()
	fmt.Fprintln(os.Stderr, "serve-api stopped.")
	if gatewayFailed.Load() {
		return fmt.Errorf("gateway journal failed; reconcile message history before resuming")
	}
	return nil
}

// A bounded queue keeps disk IO off the Telegram update handler. Overflow is
// explicit and stops the gateway; the next connection.state requests reconciliation.
func startGatewayJournal(ctx context.Context, client *telegram.Client, store *gateway.Store, cancel context.CancelFunc) <-chan struct{} {
	updates := make(chan types.StoredUpdate, 256)
	done := make(chan struct{})
	client.SetUpdateCallback(func(update types.StoredUpdate) {
		select {
		case updates <- update:
		case <-ctx.Done():
		default:
			slog.Error("Gateway update queue full; stopping for reconciliation")
			cancel()
		}
	})
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case update := <-updates:
				account := gatewayAccount(ctx, client)
				for account == "" {
					select {
					case <-ctx.Done():
						return
					case <-time.After(250 * time.Millisecond):
					}
					account = gatewayAccount(ctx, client)
				}
				if err := store.Append(ctx, account, string(update.Type), update.Data); err != nil {
					slog.Error("Gateway journal failed; stopping", "error", err)
					cancel()
					return
				}
			}
		}
	}()
	return done
}

func gatewayAccount(ctx context.Context, client *telegram.Client) string {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	status := client.GetStatus(ctx)
	if !status.Authorized || status.UserID == 0 {
		return ""
	}
	return strconv.FormatInt(status.UserID, 10)
}

func gatewayStatusEvents(ctx context.Context, client *telegram.Client, store *gateway.Store, cancel context.CancelFunc) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	last := ""
	lastAccount := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			account := gatewayAccount(ctx, client)
			state := "disconnected"
			if account != "" {
				state = "ready"
				lastAccount = account
			}
			if lastAccount != "" && state != last {
				if err := store.Append(ctx, lastAccount, "connection.state", map[string]any{"state": state, "resync_required": true}); err != nil {
					cancel()
					return
				}
				last = state
			}
		}
	}
}

// DefaultGatewayPath is separate from per-account authorization storage.
func DefaultGatewayPath() string {
	dir, err := paths.ConfigDir()
	if err != nil {
		return "gateway.db"
	}
	return filepath.Join(dir, "gateway.db")
}

func createHTTPAPIServer(
	address, secret, cors string,
	fileRoots []string,
	tgClient *telegram.Client, cancel context.CancelFunc,
) *ipc.HTTPServer {
	srv := ipc.NewHTTPServerOnAddress(address, secret, cors)
	srv.SetFileRoots(fileRoots)
	ipc.RegisterPingPong(srv)
	telegramipc.RegisterHandlers(srv, tgClient)

	registerControlHandlers(srv, tgClient, cancel)

	return srv
}
