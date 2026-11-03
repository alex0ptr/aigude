package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/alex0ptr/aigude/business"
	"github.com/alex0ptr/aigude/pkg/audio"
	"github.com/alex0ptr/aigude/pkg/firecrawl"
	"github.com/alex0ptr/aigude/pkg/llm"
	"github.com/alex0ptr/aigude/pkg/logbuffer"
	"github.com/alex0ptr/aigude/pkg/scripttoml"
	"github.com/alex0ptr/aigude/pkg/service"
	"github.com/alex0ptr/aigude/pkg/settings"
	"github.com/alex0ptr/aigude/pkg/version"
	"github.com/spf13/cobra"
)

// logLines is how many of the newest log lines the web interface can show.
const logLines = 500

func newServeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the web service",
		Long: "Start the web service.\n\n" +
			"Settings precedence, lowest to highest: built-in defaults, settings file,\n" +
			"environment variables prefixed " + settings.EnvPrefix + "_ (for example " + settings.EnvPrefix + "_LISTEN_ADDR), flags.\n" +
			"The API keys GEMINI_API_KEY and FIRECRAWL_API_KEY are read from the environment only;\n" +
			"FIRECRAWL_API_KEY is optional when firecrawl_url points to a Firecrawl of your own.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			err := runServe(cmd)
			if err != nil {
				fmt.Fprintln(os.Stderr, "aigude:", err)
			}
			return err
		},
	}
	addSettingsFlags(cmd)
	return cmd
}

// runServe checks that ffmpeg is installed and the API keys are set (the Firecrawl key only for the hosted Firecrawl), loads the settings, builds the real external
// systems and runs the service until interrupted.
func runServe(cmd *cobra.Command) error {
	logs := logbuffer.New(slog.NewTextHandler(os.Stderr, nil), logLines)
	slog.SetDefault(slog.New(logs))
	slog.Info("Starting AI Gude", "version", version.String())
	if !audio.Available() {
		slog.Error("ffmpeg not found on PATH")
		return errors.New("ffmpeg was not found on PATH; install ffmpeg and try again")
	}
	cfg, err := loadSettings(cmd)
	if err != nil {
		return err
	}

	slog.Info("Settings loaded", "listenAddr", cfg.ListenAddr, "usersDir", cfg.UsersDir, "dataDir", cfg.DataDir)

	secrets := settings.SecretsFromEnv()
	if secrets.GeminiAPIKey == "" {
		return errors.New("GEMINI_API_KEY is not set; it is read from the environment only")
	}
	if secrets.FirecrawlAPIKey == "" && isHostedFirecrawl(cfg.FirecrawlURL) {
		return errors.New("FIRECRAWL_API_KEY is not set; it is read from the environment only; " +
			"it is optional only for a Firecrawl of your own (setting firecrawl_url)")
	}
	slog.Info("Creating Gemini clients", "scriptModel", cfg.ScriptModel, "ttsModel", cfg.TTSModel)
	gemini, err := llm.NewGemini(secrets.GeminiAPIKey, cfg.ScriptModel)
	if err != nil {
		return err
	}
	tts, err := llm.NewGeminiTTS(secrets.GeminiAPIKey, cfg.TTSModel)
	if err != nil {
		return err
	}

	slog.Info("Crawling through Firecrawl", "url", cfg.FirecrawlURL, "authenticated", secrets.FirecrawlAPIKey != "")
	crawler := firecrawl.NewClient(firecrawl.ClientOpts{APIKey: secrets.FirecrawlAPIKey, BaseURL: cfg.FirecrawlURL})

	svc, err := service.New(cfg, scripttoml.New(gemini), tts, crawler, business.SystemClock{}, logs)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return svc.Run(ctx)
}

// isHostedFirecrawl tells whether the address is the hosted Firecrawl, which needs an API key. A trailing slash and the
// case of the address do not matter.
func isHostedFirecrawl(address string) bool {
	return strings.EqualFold(strings.TrimRight(address, "/"), settings.DefaultFirecrawlURL)
}
