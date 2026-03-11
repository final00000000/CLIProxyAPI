package usage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Storage handles persistent storage of usage statistics
type Storage struct {
	db *sql.DB
}

// NewStorage creates a new storage instance with SQLite backend
func NewStorage(dataDir string) (*Storage, error) {
	if dataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		dataDir = filepath.Join(home, ".cli-proxy-api")
	}

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	dbPath := filepath.Join(dataDir, "usage_stats.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	s := &Storage{db: db}
	if err := s.initSchema(); err != nil {
		db.Close()
		return nil, err
	}

	return s, nil
}

// initSchema creates the database schema
func (s *Storage) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS usage_stats (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		api_key TEXT NOT NULL,
		model TEXT NOT NULL,
		source TEXT,
		auth_index TEXT,
		input_tokens INTEGER DEFAULT 0,
		output_tokens INTEGER DEFAULT 0,
		total_tokens INTEGER DEFAULT 0,
		failed BOOLEAN DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_timestamp ON usage_stats(timestamp);
	CREATE INDEX IF NOT EXISTS idx_api_key ON usage_stats(api_key);
	CREATE INDEX IF NOT EXISTS idx_model ON usage_stats(model);

	CREATE TABLE IF NOT EXISTS daily_aggregates (
		date TEXT PRIMARY KEY,
		total_requests INTEGER DEFAULT 0,
		success_count INTEGER DEFAULT 0,
		failure_count INTEGER DEFAULT 0,
		total_tokens INTEGER DEFAULT 0,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS hourly_aggregates (
		hour INTEGER PRIMARY KEY,
		total_requests INTEGER DEFAULT 0,
		total_tokens INTEGER DEFAULT 0,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`

	_, err := s.db.Exec(schema)
	return err
}

// SaveRecord saves a usage record to the database
func (s *Storage) SaveRecord(timestamp time.Time, apiKey, model, source, authIndex string, tokens TokenStats, failed bool) error {
	query := `
		INSERT INTO usage_stats (timestamp, api_key, model, source, auth_index, input_tokens, output_tokens, total_tokens, failed)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := s.db.Exec(query, timestamp, apiKey, model, source, authIndex, tokens.InputTokens, tokens.OutputTokens, tokens.TotalTokens, failed)
	if err != nil {
		return fmt.Errorf("failed to save record: %w", err)
	}

	// Update daily aggregates
	date := timestamp.Format("2006-01-02")
	if err := s.updateDailyAggregate(date, tokens.TotalTokens, failed); err != nil {
		return err
	}

	// Update hourly aggregates
	hour := timestamp.Hour()
	if err := s.updateHourlyAggregate(hour, tokens.TotalTokens); err != nil {
		return err
	}

	return nil
}

// updateDailyAggregate updates daily statistics
func (s *Storage) updateDailyAggregate(date string, tokens int64, failed bool) error {
	query := `
		INSERT INTO daily_aggregates (date, total_requests, success_count, failure_count, total_tokens, updated_at)
		VALUES (?, 1, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(date) DO UPDATE SET
			total_requests = total_requests + 1,
			success_count = success_count + ?,
			failure_count = failure_count + ?,
			total_tokens = total_tokens + ?,
			updated_at = CURRENT_TIMESTAMP
	`
	successInc := 0
	failureInc := 0
	if failed {
		failureInc = 1
	} else {
		successInc = 1
	}
	_, err := s.db.Exec(query, date, successInc, failureInc, tokens, successInc, failureInc, tokens)
	return err
}

// updateHourlyAggregate updates hourly statistics
func (s *Storage) updateHourlyAggregate(hour int, tokens int64) error {
	query := `
		INSERT INTO hourly_aggregates (hour, total_requests, total_tokens, updated_at)
		VALUES (?, 1, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(hour) DO UPDATE SET
			total_requests = total_requests + 1,
			total_tokens = total_tokens + ?,
			updated_at = CURRENT_TIMESTAMP
	`
	_, err := s.db.Exec(query, hour, tokens, tokens)
	return err
}

// LoadAggregates loads aggregated statistics from the database
func (s *Storage) LoadAggregates() (totalRequests, successCount, failureCount, totalTokens int64, err error) {
	query := `
		SELECT COALESCE(SUM(total_requests), 0), COALESCE(SUM(success_count), 0),
		       COALESCE(SUM(failure_count), 0), COALESCE(SUM(total_tokens), 0)
		FROM daily_aggregates
	`
	err = s.db.QueryRow(query).Scan(&totalRequests, &successCount, &failureCount, &totalTokens)
	return
}

// LoadDailyStats loads daily statistics
func (s *Storage) LoadDailyStats() (map[string]int64, map[string]int64, error) {
	requestsByDay := make(map[string]int64)
	tokensByDay := make(map[string]int64)

	rows, err := s.db.Query("SELECT date, total_requests, total_tokens FROM daily_aggregates ORDER BY date DESC LIMIT 30")
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var date string
		var requests, tokens int64
		if err := rows.Scan(&date, &requests, &tokens); err != nil {
			return nil, nil, err
		}
		requestsByDay[date] = requests
		tokensByDay[date] = tokens
	}

	return requestsByDay, tokensByDay, nil
}

// LoadHourlyStats loads hourly statistics
func (s *Storage) LoadHourlyStats() (map[int]int64, map[int]int64, error) {
	requestsByHour := make(map[int]int64)
	tokensByHour := make(map[int]int64)

	rows, err := s.db.Query("SELECT hour, total_requests, total_tokens FROM hourly_aggregates")
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var hour int
		var requests, tokens int64
		if err := rows.Scan(&hour, &requests, &tokens); err != nil {
			return nil, nil, err
		}
		requestsByHour[hour] = requests
		tokensByHour[hour] = tokens
	}

	return requestsByHour, tokensByHour, nil
}

// Close closes the database connection
func (s *Storage) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
