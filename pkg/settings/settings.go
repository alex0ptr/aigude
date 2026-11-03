// Package settings loads the service-wide settings of AI Gude with Viper.
//
// Precedence, lowest to highest: built-in defaults, settings file,
// environment variables with the prefix AIGUDE_, command line flags. Unknown
// keys in the settings file are an error. API keys come from the
// environment only (see Secrets).
//
// The User Config files of the Users are read by package userconfig.
package settings

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
	_ "time/tzdata" // the configured time zone must resolve on hosts without a zoneinfo database

	"github.com/alex0ptr/aigude/pkg/llm"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// EnvPrefix is the common prefix of all settings environment variables, for
// example AIGUDE_LISTEN_ADDR.
const EnvPrefix = "AIGUDE"

// DefaultFirecrawlURL is the address of the hosted Firecrawl API, the default of firecrawl_url.
const DefaultFirecrawlURL = "https://api.firecrawl.dev"

// Settings are the service-wide settings. Changing them requires a restart.
type Settings struct {
	// TimeZone is the IANA name of the one time zone in which Schedules are
	// evaluated.
	TimeZone string `mapstructure:"time_zone"`
	// UsersDir is the folder holding one User Config file per User.
	UsersDir string `mapstructure:"users_dir"`
	// DataDir is the folder for the database and the audio files.
	DataDir string `mapstructure:"data_dir"`
	// ListenAddr is the host:port the web interface listens on.
	ListenAddr string `mapstructure:"listen_addr"`
	// DefaultLanguage is the BCP-47 language code used when a CastProfile sets none.
	DefaultLanguage string `mapstructure:"default_language"`
	// DefaultVoice is the speech voice used when a CastProfile sets none.
	DefaultVoice string `mapstructure:"default_voice"`
	// WordBudget is the Word Budget of a CastProfile that states none: the number of spoken words an Episode aims for
	// and should not exceed (about 1400 is ten minutes).
	WordBudget int `mapstructure:"word_budget"`
	// ArticleCharLimit is the number of characters of one article handed to the language model.
	ArticleCharLimit int `mapstructure:"article_char_limit"`
	// CrawlCacheTTL is how long a retrieved page is served from the crawl cache before it is retrieved again.
	CrawlCacheTTL time.Duration `mapstructure:"crawl_cache_ttl"`
	// FirecrawlURL is the address of the Firecrawl API; the hosted service by default, or a Firecrawl run by the person
	// running the service.
	FirecrawlURL string `mapstructure:"firecrawl_url"`
	// ScriptModel is the Gemini model that writes the Script; it is not checked before the first request.
	ScriptModel string `mapstructure:"script_model"`
	// TTSModel is the Gemini model that speaks the Script; it is not checked before the first request.
	TTSModel string `mapstructure:"tts_model"`
	// SchedulerMaxSleep is the longest the background work sleeps before re-checking the wall clock.
	SchedulerMaxSleep time.Duration `mapstructure:"scheduler_max_sleep"`
	// SectionPause is the silence inserted between two Sections of an Episode.
	SectionPause time.Duration `mapstructure:"section_pause"`
	// EpisodeRetention is how long an Episode is kept, counted from its creation; zero keeps Episodes for ever.
	EpisodeRetention time.Duration `mapstructure:"episode_retention"`
}

// Defaults returns the built-in value of every setting.
func Defaults() Settings {
	return Settings{
		TimeZone:          "Europe/Berlin",
		UsersDir:          "users",
		DataDir:           "data",
		ListenAddr:        "127.0.0.1:8080",
		DefaultLanguage:   "de-DE",
		DefaultVoice:      "Achernar",
		WordBudget:        1400,
		ArticleCharLimit:  12000,
		CrawlCacheTTL:     240 * time.Hour,
		FirecrawlURL:      DefaultFirecrawlURL,
		ScriptModel:       llm.DefaultGeminiModel,
		TTSModel:          llm.DefaultGeminiTTSModel,
		SchedulerMaxSleep: time.Hour,
		SectionPause:      time.Second,
		EpisodeRetention:  720 * time.Hour,
	}
}

// LoadOptions says where settings come from besides the defaults.
type LoadOptions struct {
	// File is the path of the settings file. It must exist when set; an
	// empty File means no settings file is read.
	File string
	// Flags are the parsed command line flags created by NewFlagSet. Only
	// flags that were actually given override other sources. May be nil.
	Flags *pflag.FlagSet
}

// Load reads the settings from all sources and validates them.
func Load(opts LoadOptions) (Settings, error) {
	v := viper.New()
	setDefaults(v)
	known := make(map[string]bool)
	for _, key := range v.AllKeys() {
		known[key] = true
	}

	v.SetEnvPrefix(EnvPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()

	if opts.Flags != nil {
		if err := bindFlags(v, opts.Flags, known); err != nil {
			return Settings{}, err
		}
	}

	if opts.File != "" {
		v.SetConfigFile(opts.File)
		slog.Debug("Reading settings file", "file", opts.File)
		if err := v.ReadInConfig(); err != nil {
			return Settings{}, fmt.Errorf("settings file %s: %w", opts.File, err)
		}
		if unknown := unknownKeys(v, known); len(unknown) > 0 {
			slog.Error("Settings file contains unknown keys", "file", opts.File, "keys", unknown)
			return Settings{}, fmt.Errorf("settings file %s: unknown key(s): %s (API keys belong in the environment, not in a file)",
				opts.File, strings.Join(unknown, ", "))
		}
	}

	var s Settings
	if err := v.Unmarshal(&s); err != nil {
		return Settings{}, fmt.Errorf("decode settings%s: %w", fileSuffix(opts.File), err)
	}
	if err := s.validate(); err != nil {
		slog.Error("Invalid settings", "error", err)
		return Settings{}, err
	}
	return s, nil
}

// setDefaults registers every setting with its default, which also makes the
// key known to Viper's environment and flag lookup.
func setDefaults(v *viper.Viper) {
	d := Defaults()
	v.SetDefault("time_zone", d.TimeZone)
	v.SetDefault("users_dir", d.UsersDir)
	v.SetDefault("data_dir", d.DataDir)
	v.SetDefault("listen_addr", d.ListenAddr)
	v.SetDefault("default_language", d.DefaultLanguage)
	v.SetDefault("default_voice", d.DefaultVoice)
	v.SetDefault("word_budget", d.WordBudget)
	v.SetDefault("article_char_limit", d.ArticleCharLimit)
	v.SetDefault("crawl_cache_ttl", d.CrawlCacheTTL)
	v.SetDefault("firecrawl_url", d.FirecrawlURL)
	v.SetDefault("script_model", d.ScriptModel)
	v.SetDefault("tts_model", d.TTSModel)
	v.SetDefault("scheduler_max_sleep", d.SchedulerMaxSleep)
	v.SetDefault("section_pause", d.SectionPause)
	v.SetDefault("episode_retention", d.EpisodeRetention)
}

// flagName maps a settings key to its command line flag, time_zone to time-zone.
func flagName(key string) string {
	return strings.ReplaceAll(key, "_", "-")
}

// NewFlagSet defines one flag per setting. The flag defaults are the built-in
// defaults, shown in --help; a flag overrides the file and the environment only when it is given.
func NewFlagSet() *pflag.FlagSet {
	d := Defaults()
	fs := pflag.NewFlagSet("settings", pflag.ContinueOnError)
	fs.String(flagName("time_zone"), d.TimeZone, "IANA time zone for Schedules")
	fs.String(flagName("users_dir"), d.UsersDir, "folder with one User Config file per User")
	fs.String(flagName("data_dir"), d.DataDir, "folder for the database and audio files")
	fs.String(flagName("listen_addr"), d.ListenAddr, "host:port of the web interface")
	fs.String(flagName("default_language"), d.DefaultLanguage, "default Episode language as BCP-47 code")
	fs.String(flagName("default_voice"), d.DefaultVoice, "default speech voice")
	fs.Int(flagName("word_budget"), d.WordBudget, "spoken words an Episode aims for, unless its CastProfile says otherwise")
	fs.Int(flagName("article_char_limit"), d.ArticleCharLimit, "characters of one article handed to the language model")
	fs.Duration(flagName("crawl_cache_ttl"), d.CrawlCacheTTL, "how long a retrieved page is served from the cache")
	fs.String(flagName("firecrawl_url"), d.FirecrawlURL, "address of the Firecrawl API (FIRECRAWL_API_KEY is required for the hosted default only)")
	fs.String(flagName("script_model"), d.ScriptModel, "Gemini model that writes the Script")
	fs.String(flagName("tts_model"), d.TTSModel, "Gemini model that speaks the Script")
	fs.Duration(flagName("scheduler_max_sleep"), d.SchedulerMaxSleep, "longest scheduler sleep before re-checking the clock")
	fs.Duration(flagName("section_pause"), d.SectionPause, "silence between two Sections")
	fs.Duration(flagName("episode_retention"), d.EpisodeRetention, "how long an Episode is kept before it is deleted; 0 keeps them for ever")
	return fs
}

// unknownKeys returns the sorted keys Viper knows that are not settings. At
// this point defaults, environment and flags only contribute known keys, so
// anything else comes from the settings file.
func unknownKeys(v *viper.Viper, known map[string]bool) []string {
	var unknown []string
	for _, key := range v.AllKeys() {
		if !known[key] {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	return unknown
}

// bindFlags makes every setting flag of fs visible to Viper at the top of the
// precedence order. Flags that are not settings, such as --config, are skipped.
func bindFlags(v *viper.Viper, fs *pflag.FlagSet, known map[string]bool) error {
	var bindErr error
	fs.VisitAll(func(f *pflag.Flag) {
		key := strings.ReplaceAll(f.Name, "-", "_")
		if !known[key] {
			return
		}
		if err := v.BindPFlag(key, f); err != nil && bindErr == nil {
			bindErr = fmt.Errorf("bind flag --%s: %w", f.Name, err)
		}
	})
	return bindErr
}

// validate checks that every setting has a usable value and names the
// offending setting in the error.
func (s Settings) validate() error {
	var errs []error
	if _, err := time.LoadLocation(s.TimeZone); err != nil || s.TimeZone == "" || s.TimeZone == "Local" {
		errs = append(errs, fmt.Errorf("time_zone %q is not a known IANA time zone (\"Local\" is not accepted: the time zone must not depend on the host)", s.TimeZone))
	}
	if _, _, err := net.SplitHostPort(s.ListenAddr); err != nil {
		errs = append(errs, fmt.Errorf("listen_addr %q must look like host:port: %w", s.ListenAddr, err))
	}
	if s.UsersDir == "" {
		errs = append(errs, errors.New("users_dir must not be empty"))
	}
	if s.DataDir == "" {
		errs = append(errs, errors.New("data_dir must not be empty"))
	}
	if s.DefaultLanguage == "" {
		errs = append(errs, errors.New("default_language must not be empty"))
	}
	if s.DefaultVoice == "" {
		errs = append(errs, errors.New("default_voice must not be empty"))
	}
	if s.WordBudget <= 0 {
		errs = append(errs, fmt.Errorf("word_budget must be positive, got %d", s.WordBudget))
	}
	if s.ArticleCharLimit <= 0 {
		errs = append(errs, fmt.Errorf("article_char_limit must be positive, got %d", s.ArticleCharLimit))
	}
	if s.CrawlCacheTTL <= 0 {
		errs = append(errs, fmt.Errorf("crawl_cache_ttl must be positive, got %s", s.CrawlCacheTTL))
	}
	if u, err := url.Parse(s.FirecrawlURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		errs = append(errs, fmt.Errorf("firecrawl_url %q must be an http or https address", s.FirecrawlURL))
	}
	if s.ScriptModel == "" {
		errs = append(errs, errors.New("script_model must not be empty"))
	}
	if s.TTSModel == "" {
		errs = append(errs, errors.New("tts_model must not be empty"))
	}
	if s.SchedulerMaxSleep <= 0 {
		errs = append(errs, fmt.Errorf("scheduler_max_sleep must be positive, got %s", s.SchedulerMaxSleep))
	}
	if s.SectionPause < 0 {
		errs = append(errs, fmt.Errorf("section_pause must not be negative, got %s", s.SectionPause))
	}
	if s.EpisodeRetention < 0 {
		errs = append(errs, fmt.Errorf("episode_retention must not be negative, got %s", s.EpisodeRetention))
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("invalid settings: %w", err)
	}
	return nil
}

// Secrets are the API keys of the external services. They are read from the
// environment and are kept apart from Settings, which is what gets read from
// files, logged and stored.
type Secrets struct {
	// GeminiAPIKey is the key for the language model and speech (GEMINI_API_KEY).
	GeminiAPIKey string
	// FirecrawlAPIKey is the key for the Crawler (FIRECRAWL_API_KEY).
	FirecrawlAPIKey string
}

// SecretsFromEnv reads the API keys from the environment.
func SecretsFromEnv() Secrets {
	return Secrets{
		GeminiAPIKey:    os.Getenv("GEMINI_API_KEY"),
		FirecrawlAPIKey: os.Getenv("FIRECRAWL_API_KEY"),
	}
}

// fileSuffix renders the origin of a decoding error for messages.
func fileSuffix(file string) string {
	if file == "" {
		return ""
	}
	return " (file " + file + ")"
}
