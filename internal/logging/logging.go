package logging

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/rs/zerolog"
)

const (
	OutputConsole = "console"
	OutputFile    = "file"
	OutputBoth    = "both"
	FormatJSON    = "json"
	FormatConsole = "console"
)

type Config struct {
	Output  string
	File    string
	Level   string
	Format  string
	Service string
}

type Bundle struct {
	Logger        zerolog.Logger
	AsynqLogger   asynq.Logger
	AsynqLogLevel asynq.LogLevel
	file          *os.File
}

func New(config Config) (*Bundle, error) {
	output := strings.ToLower(strings.TrimSpace(config.Output))
	format := strings.ToLower(strings.TrimSpace(config.Format))
	levelName := strings.ToLower(strings.TrimSpace(config.Level))
	if output == "" {
		output = OutputConsole
	}
	if format == "" {
		format = FormatJSON
	}
	if levelName == "" {
		levelName = zerolog.InfoLevel.String()
	}

	level, err := zerolog.ParseLevel(levelName)
	if err != nil {
		return nil, fmt.Errorf("invalid LOG_LEVEL %q: %w", config.Level, err)
	}

	writer, file, err := outputWriter(output, strings.TrimSpace(config.File))
	if err != nil {
		return nil, err
	}
	if format == FormatConsole {
		writer = zerolog.ConsoleWriter{
			Out:        writer,
			NoColor:    true,
			TimeFormat: time.RFC3339,
		}
	} else if format != FormatJSON {
		_ = closeFile(file)
		return nil, fmt.Errorf("invalid LOG_FORMAT %q: expected json or console", config.Format)
	}
	writer = zerolog.SyncWriter(writer)

	zerolog.TimeFieldFormat = time.RFC3339Nano
	logger := zerolog.New(writer).Level(level).With().
		Timestamp().
		Str("service", strings.TrimSpace(config.Service)).
		Logger()
	asynqLogger := &asynqLogAdapter{logger: logger.With().Str("component", "asynq").Logger()}

	return &Bundle{
		Logger:        logger,
		AsynqLogger:   asynqLogger,
		AsynqLogLevel: toAsynqLogLevel(level),
		file:          file,
	}, nil
}

func toAsynqLogLevel(level zerolog.Level) asynq.LogLevel {
	switch {
	case level <= zerolog.DebugLevel:
		return asynq.DebugLevel
	case level == zerolog.InfoLevel:
		return asynq.InfoLevel
	case level == zerolog.WarnLevel:
		return asynq.WarnLevel
	case level == zerolog.ErrorLevel:
		return asynq.ErrorLevel
	default:
		return asynq.FatalLevel
	}
}

func (bundle *Bundle) Close() error {
	if bundle == nil {
		return nil
	}
	return closeFile(bundle.file)
}

func outputWriter(output, filePath string) (io.Writer, *os.File, error) {
	switch output {
	case OutputConsole:
		return os.Stdout, nil, nil
	case OutputFile, OutputBoth:
		if filePath == "" {
			return nil, nil, errors.New("LOG_FILE is required when LOG_OUTPUT is file or both")
		}
		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			return nil, nil, fmt.Errorf("create log directory: %w", err)
		}
		file, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, nil, fmt.Errorf("open log file %s: %w", filePath, err)
		}
		if output == OutputBoth {
			return io.MultiWriter(os.Stdout, file), file, nil
		}
		return file, file, nil
	default:
		return nil, nil, fmt.Errorf("invalid LOG_OUTPUT %q: expected console, file or both", output)
	}
}

func closeFile(file *os.File) error {
	if file == nil {
		return nil
	}
	return file.Close()
}

type asynqLogAdapter struct {
	logger zerolog.Logger
}

func (adapter *asynqLogAdapter) Debug(args ...interface{}) {
	adapter.logger.Debug().Msg(fmt.Sprint(args...))
}

func (adapter *asynqLogAdapter) Info(args ...interface{}) {
	adapter.logger.Info().Msg(fmt.Sprint(args...))
}

func (adapter *asynqLogAdapter) Warn(args ...interface{}) {
	adapter.logger.Warn().Msg(fmt.Sprint(args...))
}

func (adapter *asynqLogAdapter) Error(args ...interface{}) {
	adapter.logger.Error().Msg(fmt.Sprint(args...))
}

func (adapter *asynqLogAdapter) Fatal(args ...interface{}) {
	adapter.logger.Fatal().Msg(fmt.Sprint(args...))
}
