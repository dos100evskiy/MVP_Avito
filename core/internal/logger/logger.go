package logger

import (
	"log/slog"
	"os"
)

// New создаёт JSON-логгер (log/slog из stdlib — осознанно не тащим zap/zerolog
// ради минимализма MVP, но интерфейс *slog.Logger легко заменить при росте проекта).
func New() *slog.Logger {
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	return slog.New(h)
}
